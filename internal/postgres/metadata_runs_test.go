package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/metadata"
)

type runFixture struct {
	*adviceFixture
	coordinator *metadata.Coordinator
	handlers    map[string]delivery.Handler
}

func newRunFixture(t *testing.T) *runFixture {
	t.Helper()
	a := newAdviceFixture(t)
	c, err := metadata.NewCoordinator(a.f.store, a.service, a.pictures, a.inspector, metadata.RunOptions{Session: "fixture-session", OwnerID: 123, Management: -100})
	if err != nil {
		t.Fatal(err)
	}
	a.f.managementHandlers["sync_metadata"] = c.Admit
	handlers := a.f.dispatcher.Handlers()
	for kind, h := range c.Handlers() {
		handlers[kind] = h
	}
	handlers["control-sync-report"] = a.reports.Send
	return &runFixture{adviceFixture: a, coordinator: c, handlers: handlers}
}
func (r *runFixture) drain(t *testing.T, finish int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	completed := 0
	var mu sync.Mutex
	worker, err := delivery.NewWorker(r.f.store, r.handlers, delivery.WorkerOptions{Concurrency: 3, Lease: 9 * time.Second, RenewEvery: 500 * time.Millisecond, StoreTimeout: 2 * time.Second, PollEvery: 5 * time.Millisecond, ShutdownTimeout: 2 * time.Second, Report: func(report delivery.Report) {
		var kind string
		if err := r.f.store.pool.QueryRow(context.Background(), "SELECT kind FROM jobs WHERE id=$1", report.JobID).Scan(&kind); err != nil {
			return
		}
		if kind == "control-sync-report" && report.State == delivery.Done {
			mu.Lock()
			completed++
			if completed >= finish {
				cancel()
			}
			mu.Unlock()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := completed
	mu.Unlock()
	if got != finish {
		t.Fatalf("reports=%d want %d", got, finish)
	}
}
func (r *runFixture) admit(t *testing.T, key, scope, id string) string {
	t.Helper()
	job := r.enqueue(t, key, scope, id)
	if result := r.f.run(t, job, "management", r.coordinator.Admit); result.State != delivery.Done {
		t.Fatal(result)
	}
	var run string
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT run_id FROM metadata_admissions WHERE request_key=$1", key).Scan(&run); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestMetadataRunNativeCommandExecutesFullWorkflowAndReport(t *testing.T) {
	r := newRunFixture(t)
	r.f.route(t, "123@g.us", -200, 0)
	if result := operatorCommand(t, r.f, 9000, "master", -200, 0, "/sync"); result.State != delivery.Done {
		t.Fatal(result)
	}
	r.drain(t, 1)
	if len(r.uploads) != 1 || r.groupReads != 1 {
		t.Fatal(len(r.uploads), r.groupReads)
	}
	r.f.mu.Lock()
	calls := append([]tgTextCall(nil), r.f.tgCalls...)
	r.f.mu.Unlock()
	if len(calls) != 2 || !strings.Contains(calls[1].Text, "1 groups, 0 contacts, 3 fields updated") || calls[1].Chat != -200 {
		t.Fatal(calls)
	}
	var state string
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT state FROM metadata_runs").Scan(&state); err != nil || state != "completed" {
		t.Fatal(state, err)
	}
}

func TestMetadataRunCoalescesActiveAllAndDeduplicatesSharedContactFanout(t *testing.T) {
	r := newRunFixture(t)
	r.f.route(t, "123@g.us", -200, 0)
	r.f.route(t, "124@g.us", -201, 0)
	r.bindBot(t, "200", "12345000000001@lid", "Alice")
	r.groupByID = map[string]string{"124@g.us": `{"id":"124@g.us","name":"Second","participants":[{"id":"12345000000001@lid"}]}`}
	run := r.admit(t, "global-run", "all", "")
	joined := r.admit(t, "scoped-waiter", "group", "123@g.us")
	if joined != run {
		t.Fatal("active all run did not coalesce", run, joined)
	}
	r.drain(t, 1)
	var groups, contacts int
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FILTER(WHERE kind='group'),count(*) FILTER(WHERE kind='contact') FROM metadata_run_targets").Scan(&groups, &contacts); err != nil || groups != 2 || contacts != 1 {
		t.Fatal(groups, contacts, err)
	}
	if r.contactReads != 1 {
		t.Fatal("shared contact fetched repeatedly", r.contactReads)
	}
}

func TestMetadataRunGroupDiscoversBoundContactWithoutDirectRoute(t *testing.T) {
	r := newRunFixture(t)
	r.f.route(t, "123@g.us", -200, 0)
	r.bindBot(t, "200", "12345000000001@lid", "Alice")
	r.admit(t, "group-discovery", "group", "123@g.us")
	r.drain(t, 1)
	if r.contactReads != 1 {
		t.Fatal(r.contactReads)
	}
	var contacts int
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM metadata_run_targets WHERE kind='contact'").Scan(&contacts); err != nil || contacts != 1 {
		t.Fatal(contacts, err)
	}
}

func TestMetadataRunKnownFieldRefusalContinuesOtherUpdates(t *testing.T) {
	r := newRunFixture(t)
	r.f.route(t, "123@g.us", -200, 0)
	r.url = nil
	r.refuseMethod = "setChatTitle"
	r.admit(t, "field-refusal", "group", "123@g.us")
	r.drain(t, 1)
	var outcome string
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT outcome FROM metadata_run_targets").Scan(&outcome); err != nil || outcome != "warning" {
		t.Fatal(outcome, err)
	}
	var report []byte
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT payload FROM metadata_reports").Scan(&report); err != nil {
		t.Fatal(err)
	}
	var parsed metadata.Report
	if err := json.Unmarshal(report, &parsed); err != nil || parsed.Changed != 1 || parsed.Warnings != 1 || parsed.SkippedPhotos != 1 {
		t.Fatal(parsed, err)
	}
}

func TestMetadataRunCronPersistsMinuteAcrossConsumersAndIsConfiguredGated(t *testing.T) {
	r := newRunFixture(t)
	now := time.Date(2026, 11, 11, 0, 30, 45, 0, time.UTC)
	if _, created, err := r.coordinator.Tick(t.Context(), "fixture-account", now, false); err != nil || created {
		t.Fatal(created, err)
	}
	run, created, err := r.coordinator.Tick(t.Context(), "fixture-account", now, true)
	if err != nil || !created {
		t.Fatal(run, created, err)
	}
	reopened, err := Open(t.Context(), r.f.store.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	c, err := metadata.NewCoordinator(reopened, r.service, r.pictures, r.inspector, metadata.RunOptions{Session: "fixture-session", OwnerID: 123, Management: -100})
	if err != nil {
		t.Fatal(err)
	}
	again, created, err := c.Tick(t.Context(), "fixture-account", now.Add(10*time.Second), true)
	if err != nil || created || again != run {
		t.Fatal(again, created, err)
	}
	r.drain(t, 1)
	var ticks int
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM metadata_cron_ticks").Scan(&ticks); err != nil || ticks != 1 {
		t.Fatal(ticks, err)
	}
}

func TestMetadataRunUncertainSourcePreventsCompletionAndLaterSourceWork(t *testing.T) {
	r := newRunFixture(t)
	r.f.route(t, "123@g.us", -200, 0)
	r.dropAt = 1
	run := r.admit(t, "uncertain-run", "group", "123@g.us")
	var start int64
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", "sync-start:"+run).Scan(&start); err != nil {
		t.Fatal(err)
	}
	if result := r.f.run(t, start, "sync-start", r.coordinator.Start); result.State != delivery.Done {
		t.Fatal(result)
	}
	var target int64
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT job_id FROM metadata_run_targets").Scan(&target); err != nil {
		t.Fatal(err)
	}
	if result := r.f.run(t, target, "sync-target", r.coordinator.Target); result.State != delivery.Uncertain {
		t.Fatal(result)
	}
	var seal int64
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", "sync-seal:"+run).Scan(&seal); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := r.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", seal); err != nil {
			t.Fatal(err)
		}
		if result := r.f.run(t, seal, "sync-seal", r.coordinator.Seal); result.State != delivery.Pending || result.Reason != "stream_requeued" {
			t.Fatal(result)
		}
	}
	var attempts int
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT attempts FROM jobs WHERE id=$1", seal).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatal(attempts, err)
	}
	var reports int
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM metadata_reports").Scan(&reports); err != nil || reports != 0 {
		t.Fatal(reports, err)
	}
	if _, err := r.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp()+interval '1 hour' WHERE id=$1", seal); err != nil {
		t.Fatal(err)
	}
	var stream string
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT stream FROM jobs WHERE id=$1", target).Scan(&stream); err != nil {
		t.Fatal(err)
	}
	for _, event := range []delivery.Event{
		{AccountID: "fixture-account", Dedup: "later-source", Stream: stream, Kind: "sync-target", Payload: json.RawMessage(`{}`)},
		{AccountID: "fixture-account", Dedup: "independent-chat", Stream: "chat:independent", Kind: "wa", Payload: json.RawMessage(`{}`)},
	} {
		if _, err := r.f.store.Enqueue(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	chat := claim(t, r.f.store)
	if chat.Dedup != "independent-chat" {
		t.Fatal("metadata uncertainty blocked unrelated chat", chat)
	}
	finish(t, r.f.store, chat)
	if _, err := r.f.store.Claim(t.Context(), time.Minute); !errors.Is(err, delivery.ErrNoJob) {
		t.Fatal("later source work bypassed uncertain metadata", err)
	}
	r.pmu.Lock()
	defer r.pmu.Unlock()
	if len(r.uploads) != 1 {
		t.Fatal("uncertain photo retried", len(r.uploads))
	}
}

func TestMetadataRunTargetProviderRetryBudgetRetainsWarningAndOtherWork(t *testing.T) {
	r := newRunFixture(t)
	r.f.route(t, "123@g.us", -200, 0)
	r.group = `{"id":"123@g.us","name":"Broken","participants":null}`
	run := r.admit(t, "budget-run", "group", "123@g.us")
	var start int64
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", "sync-start:"+run).Scan(&start); err != nil {
		t.Fatal(err)
	}
	if result := r.f.run(t, start, "sync-start", r.coordinator.Start); result.State != delivery.Done {
		t.Fatal(result)
	}
	var target int64
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT job_id FROM metadata_run_targets").Scan(&target); err != nil {
		t.Fatal(err)
	}
	if _, err := r.f.store.pool.Exec(t.Context(), "UPDATE jobs SET attempts=3 WHERE id=$1", target); err != nil {
		t.Fatal(err)
	}
	if result := r.f.run(t, target, "sync-target", r.coordinator.Target); result.State != delivery.Done {
		t.Fatal(result)
	}
	r.drain(t, 1)
	var report []byte
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT payload FROM metadata_reports").Scan(&report); err != nil {
		t.Fatal(err)
	}
	var parsed metadata.Report
	if err := json.Unmarshal(report, &parsed); err != nil || parsed.Warnings != 1 || parsed.Groups != 1 {
		t.Fatal(parsed, err)
	}
}

func TestMetadataRunAdmissionAndTargetClaimsRemainFenced(t *testing.T) {
	r := newRunFixture(t)
	r.enqueue(t, "claim-run", "all", "")
	j := claim(t, r.f.store)
	foreign := j
	foreign.AccountID = "other-account"
	_, err := r.f.store.AdmitMetadataRun(t.Context(), foreign, metadata.RunOptions{Session: "fixture-session", OwnerID: 123, Management: -100})
	if !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
}

func TestMetadataRunConcurrentAdmissionsCoalesceAtomically(t *testing.T) {
	r := newRunFixture(t)
	r.enqueue(t, "concurrent-one", "all", "")
	r.enqueue(t, "concurrent-two", "all", "")
	first, second := claim(t, r.f.store), claim(t, r.f.store)
	results := make(chan string, 2)
	failures := make(chan error, 2)
	for _, j := range []delivery.Job{first, second} {
		go func(j delivery.Job) {
			id, err := r.f.store.AdmitMetadataRun(t.Context(), j, metadata.RunOptions{Session: "fixture-session", OwnerID: 123, Management: -100})
			results <- id
			failures <- err
		}(j)
	}
	a, b := <-results, <-results
	if err := <-failures; err != nil {
		t.Fatal(err)
	}
	if err := <-failures; err != nil {
		t.Fatal(err)
	}
	if a == "" || a != b {
		t.Fatal(a, b)
	}
	var runs int
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM metadata_runs").Scan(&runs); err != nil || runs != 1 {
		t.Fatal(runs, err)
	}
}

func TestMetadataRunAdmissionFailureRollsBackStartAndMembership(t *testing.T) {
	r := newRunFixture(t)
	if _, err := r.f.store.pool.Exec(t.Context(), `CREATE FUNCTION reject_run_admission() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture admission'; END $$; CREATE TRIGGER reject_admission BEFORE INSERT ON metadata_admissions FOR EACH ROW EXECUTE FUNCTION reject_run_admission()`); err != nil {
		t.Fatal(err)
	}
	job := r.enqueue(t, "rollback-run", "all", "")
	if result := r.f.run(t, job, "management", r.coordinator.Admit); result.State != delivery.Pending {
		t.Fatal(result)
	}
	for _, table := range []string{"metadata_runs", "metadata_admissions", "metadata_run_requests", "metadata_run_targets"} {
		var count int
		if err := r.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
	var starts int
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='sync-start'").Scan(&starts); err != nil || starts != 0 {
		t.Fatal(starts, err)
	}
}

func TestMetadataRunConcurrentCronRecordsOneMinute(t *testing.T) {
	r := newRunFixture(t)
	at := time.Date(2026, 11, 11, 0, 30, 0, 0, time.UTC)
	created := make(chan bool, 6)
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		go func() {
			_, ok, err := r.coordinator.Tick(t.Context(), "fixture-account", at, true)
			created <- ok
			errs <- err
		}()
	}
	total := 0
	for i := 0; i < 6; i++ {
		if <-created {
			total++
		}
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if total != 1 {
		t.Fatal("same minute admitted repeatedly", total)
	}
}

func (r *runFixture) startGroup(t *testing.T, run string) int64 {
	t.Helper()
	var start, target int64
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", "sync-start:"+run).Scan(&start); err != nil {
		t.Fatal(err)
	}
	if result := r.f.run(t, start, "sync-start", r.coordinator.Start); result.State != delivery.Done {
		t.Fatal(result)
	}
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT job_id FROM metadata_run_targets WHERE run_id=$1 AND kind='group'", run).Scan(&target); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestMetadataRunOutcomeFailureReopensWithoutRepeatingConfirmedMutations(t *testing.T) {
	r := newRunFixture(t)
	r.f.route(t, "123@g.us", -200, 0)
	run := r.admit(t, "outcome-recovery", "group", "123@g.us")
	target := r.startGroup(t, run)
	if _, err := r.f.store.pool.Exec(t.Context(), `CREATE FUNCTION reject_run_outcome() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture outcome'; END $$; CREATE TRIGGER reject_outcome BEFORE UPDATE ON metadata_run_targets FOR EACH ROW EXECUTE FUNCTION reject_run_outcome()`); err != nil {
		t.Fatal(err)
	}
	if result := r.f.run(t, target, "sync-target", r.coordinator.Target); result.State != delivery.Pending {
		t.Fatal(result)
	}
	var outcome string
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT outcome FROM metadata_run_targets WHERE job_id=$1", target).Scan(&outcome); err != nil || outcome != "queued" {
		t.Fatal(outcome, err)
	}
	if _, err := r.f.store.pool.Exec(t.Context(), "DROP TRIGGER reject_outcome ON metadata_run_targets"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), r.f.store.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	text, err := metadata.New(reopened, r.wa, r.clients, "fixture-session", -100)
	if err != nil {
		t.Fatal(err)
	}
	pictures, err := metadata.NewPictures(reopened, r.pwa, r.pclients, r.download, "fixture-session", -100, metadata.MaxPictureBytes)
	if err != nil {
		t.Fatal(err)
	}
	master, err := r.pclients(t.Context(), "fixture-account", "master")
	if err != nil {
		t.Fatal(err)
	}
	advice, err := metadata.NewInspector(reopened, master, r.settings)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := metadata.NewCoordinator(reopened, text, pictures, advice, metadata.RunOptions{Session: "fixture-session", OwnerID: 123, Management: -100})
	if err != nil {
		t.Fatal(err)
	}
	r.retry(t, target)
	if result := r.f.run(t, target, "sync-target", restored.Target); result.State != delivery.Done {
		t.Fatal(result)
	}
	r.drain(t, 1)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pmu.Lock()
	defer r.pmu.Unlock()
	if len(r.calls) != 2 || len(r.uploads) != 1 || r.groupReads != 1 || r.downloads != 1 {
		t.Fatal("confirmed work repeated", len(r.calls), len(r.uploads), r.groupReads, r.downloads)
	}
}

func TestMetadataRunPartialInspectionRetainsAdviceAndUnavailablePhotoCount(t *testing.T) {
	r := newRunFixture(t)
	r.f.route(t, "123@g.us", -200, 0)
	r.bindBot(t, "200", "12345000000001@lid", "Alice")
	r.bindBot(t, "201", "2@s.whatsapp.net", "Bob")
	r.group = `{"id":"123@g.us","name":"New group","participants":[{"id":"12345000000001@lid"},{"id":"2@s.whatsapp.net"}]}`
	r.contactByID = map[string]string{"2@c.us": `{"id":"2@c.us","name":"Bob"}`}
	r.url = nil
	r.memberErrors = map[string]int{"201": 403}
	r.admit(t, "partial-inspection", "group", "123@g.us")
	r.drain(t, 1)
	var raw []byte
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT payload FROM metadata_reports").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var report metadata.Report
	if err := json.Unmarshal(raw, &report); err != nil || report.Groups != 1 || report.Contacts != 2 || report.Warnings != 1 || report.MissingBots != 1 || report.SkippedPhotos != 3 {
		t.Fatal(report, err)
	}
}

func TestMetadataRunGlobalDiscoverySkipsMutedGroupsButKeepsBoundContacts(t *testing.T) {
	r := newRunFixture(t)
	r.f.route(t, "123@g.us", -200, 0)
	r.f.route(t, "124@g.us", -201, 0)
	r.bindBot(t, "200", "12345000000001@lid", "Alice")
	if _, err := r.f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET mode='muted' WHERE whatsapp_chat='124@g.us'"); err != nil {
		t.Fatal(err)
	}
	run := r.admit(t, "global-muted", "all", "")
	r.startGroup(t, run)
	var groups, contacts int
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FILTER(WHERE kind='group'),count(*) FILTER(WHERE kind='contact') FROM metadata_run_targets WHERE run_id=$1", run).Scan(&groups, &contacts); err != nil || groups != 1 || contacts != 1 {
		t.Fatal(groups, contacts, err)
	}
}
