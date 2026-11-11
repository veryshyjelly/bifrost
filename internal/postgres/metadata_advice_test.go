package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"net/url"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/metadata"
)

type adviceFixture struct {
	*pictureFixture
	inspector *metadata.Inspector
	reports   *metadata.Reports
	settings  control.SyncSettings
}

func newAdviceFixture(t *testing.T) *adviceFixture {
	t.Helper()
	a := &adviceFixture{pictureFixture: newPictureFixture(t, true), settings: control.SyncSettings{Session: "fixture-session", OwnerID: 123, Management: -100, CommandTTL: 60, ControlTTL: 600}}
	master, err := a.pclients(t.Context(), "fixture-account", "master")
	if err != nil {
		t.Fatal(err)
	}
	a.inspector, err = metadata.NewInspector(a.f.store, master, a.settings)
	if err != nil {
		t.Fatal(err)
	}
	replies, err := control.New(a.f.store, a.f.replyClients, -100)
	if err != nil {
		t.Fatal(err)
	}
	a.reports, err = metadata.NewReports(a.f.store, replies, a.settings)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func (a *adviceFixture) bindBot(t *testing.T, id, contact, name string) {
	t.Helper()
	a.f.bind(t, id, contact)
	if _, err := a.f.store.pool.Exec(t.Context(), "UPDATE bridge_bots SET username=$1 WHERE bot_id=$2", "bot_"+id, id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.f.store.pool.Exec(t.Context(), "INSERT INTO contact_profiles(account_id,person_id,name,metadata_synced) VALUES('fixture-account',$1,$2,true) ON CONFLICT(account_id,person_id) DO UPDATE SET name=EXCLUDED.name", contact, name); err != nil {
		t.Fatal(err)
	}
}
func (a *adviceFixture) target(t *testing.T, run, scope, id string) int64 {
	t.Helper()
	job := a.enqueue(t, "target:"+run+":"+id, scope, id, run)
	if report := a.f.run(t, job, "management", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		if _, err := a.service.Prepare(ctx, j); err != nil {
			return err
		}
		if err := a.service.ApplyText(ctx, j, e); err != nil {
			return err
		}
		if _, err := a.pictures.Prepare(ctx, j); err != nil {
			return err
		}
		if err := a.pictures.Apply(ctx, j, e); err != nil {
			return err
		}
		_, err := a.inspector.Inspect(ctx, j)
		return err
	}); report.State != delivery.Done {
		t.Fatal(report)
	}
	return job
}
func (a *adviceFixture) report(t *testing.T, in metadata.ReportRequest) delivery.Report {
	t.Helper()
	if in.Session == "" {
		in.Session = a.settings.Session
	}
	if in.Source == "" {
		in.Source = "command"
	}
	raw, _ := json.Marshal(in)
	key := "sync-finish:" + in.RunID
	if _, err := a.f.store.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: key, Stream: key, Kind: "control-sync-report", Payload: raw}); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := a.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return a.f.run(t, id, "control-sync-report", a.reports.Send)
}
func (a *adviceFixture) inspection(t *testing.T, job int64) metadata.Inspection {
	t.Helper()
	var raw []byte
	if err := a.f.store.pool.QueryRow(t.Context(), "SELECT payload FROM metadata_inspections WHERE job_id=$1", job).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var result metadata.Inspection
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestMetadataAdviceMissingBotAndScopedPermissionSurviveReport(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	a.forbid = true
	a.bindBot(t, "200", "12345000000001@lid", "Alice")
	job := a.target(t, "advice", "group", "123@g.us")
	result := a.inspection(t, job)
	if result.Permission == nil || len(result.Missing) != 1 || result.Missing[0].Name != "Alice" || result.Missing[0].ID != "200" {
		t.Fatal(result)
	}
	if result.Permission.OwnerID != 123 || result.Permission.AccountID != "fixture-account" || result.Permission.Session != "fixture-session" || result.Permission.ChatID != -200 {
		t.Fatal(result.Permission)
	}
	if a.urlReads != 0 || len(a.uploads) != 0 {
		t.Fatal("permission inspection wrote/fetched source photo")
	}
	if r := a.report(t, metadata.ReportRequest{RunID: "advice", TargetJobs: []int64{job}}); r.State != delivery.Done {
		t.Fatal(r)
	}
	call := a.f.tgCalls[len(a.f.tgCalls)-1]
	if !strings.Contains(call.Text, "1 warnings; 1 contact bots need adding") {
		t.Fatal(call.Text)
	}
	var markup struct {
		InlineKeyboard [][]struct{ Text, URL string } `json:"inline_keyboard"`
	}
	if err := json.Unmarshal([]byte(call.Markup), &markup); err != nil || len(markup.InlineKeyboard) != 2 {
		t.Fatal(call.Markup, err)
	}
	if markup.InlineKeyboard[0][0].Text != "Add Alice" || markup.InlineKeyboard[0][0].URL != "https://t.me/bot_200?startgroup=bridge" || markup.InlineKeyboard[1][0].URL != result.PermissionLink {
		t.Fatal(markup)
	}
	parsed, err := url.Parse(result.PermissionLink)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("startgroup") != "sync_"+result.Permission.ID || !strings.Contains(result.PermissionLink, "admin=change_info+delete_messages+invite_users") {
		t.Fatal(result.PermissionLink)
	}
}

func TestMetadataGrantLinkOnlyQueuesTheOriginalGroup(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	a.forbid = true
	job := a.target(t, "grant", "group", "123@g.us")
	permission := a.inspection(t, job).Permission
	if permission == nil {
		t.Fatal("missing permission grant")
	}
	command := "/start sync_" + permission.ID
	if r := operatorCommand(t, a.f, 8000, "master", -999, 0, command); r.State != delivery.Done {
		t.Fatal(r)
	}
	if a.f.tgCalls[len(a.f.tgCalls)-1].Text != control.SyncPermissionExpired {
		t.Fatal(a.f.tgCalls)
	}
	if r := operatorCommand(t, a.f, 8001, "master", -200, 0, command); r.State != delivery.Done {
		t.Fatal(r)
	}
	if a.f.tgCalls[len(a.f.tgCalls)-1].Text != control.SyncPermissionQueued {
		t.Fatal(a.f.tgCalls)
	}
	var payload []byte
	if err := a.f.store.pool.QueryRow(t.Context(), "SELECT payload FROM jobs WHERE dedup='management:tg:master:8001'").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var source struct {
		Data struct {
			Scope, ID string
			Notify    struct {
				Bot  string
				Chat int64
			}
		}
	}
	if err := json.Unmarshal(payload, &source); err != nil || source.Data.Scope != "group" || source.Data.ID != "123@g.us" || source.Data.Notify.Chat != -200 {
		t.Fatal(string(payload), err)
	}
}

func TestMetadataGrantExpirySessionAndReturnGenerationRejectWithoutAdmission(t *testing.T) {
	for _, change := range []string{"expiry", "session", "account", "owner", "generation"} {
		t.Run(change, func(t *testing.T) {
			a := newAdviceFixture(t)
			a.f.route(t, "123@g.us", -200, 0)
			a.forbid = true
			job := a.target(t, "grant-"+change, "group", "123@g.us")
			permission := a.inspection(t, job).Permission
			if change == "generation" {
				if _, err := a.f.store.pool.Exec(t.Context(), "INSERT INTO mirror_returns(account_id,whatsapp_chat,return_key) VALUES('fixture-account','123@g.us','new-generation')"); err != nil {
					t.Fatal(err)
				}
			} else {
				field, value := "ExpiresAt", json.RawMessage(`1`)
				switch change {
				case "session":
					field, value = "Session", json.RawMessage(`"other-session"`)
				case "account":
					field, value = "AccountID", json.RawMessage(`"other-account"`)
				case "owner":
					field, value = "OwnerID", json.RawMessage(`999`)
				}
				if _, err := a.f.store.pool.Exec(t.Context(), "UPDATE metadata_permissions SET payload=jsonb_set(payload,$1::text[],$2::jsonb)", []string{field}, value); err != nil {
					t.Fatal(err)
				}
			}
			if r := operatorCommand(t, a.f, 8100, "master", -200, 0, "/start sync_"+permission.ID); r.State != delivery.Done {
				t.Fatal(r)
			}
			if a.f.tgCalls[len(a.f.tgCalls)-1].Text != control.SyncPermissionExpired {
				t.Fatal(a.f.tgCalls)
			}
			var count int
			if err := a.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE dedup='management:tg:master:8100'").Scan(&count); err != nil || count != 0 {
				t.Fatal(count, err)
			}
		})
	}
}

func TestMetadataAdvicePreservesNativeMembershipRulesAnd400Fallback(t *testing.T) {
	for _, role := range []string{"member", "administrator", "creator", "restricted", "left", "kicked", "400"} {
		t.Run(role, func(t *testing.T) {
			a := newAdviceFixture(t)
			a.f.route(t, "123@g.us", -200, 0)
			a.url = nil
			a.bindBot(t, "200", "12345000000001@lid", "Alice")
			if role == "400" {
				a.memberErrors = map[string]int{"200": 400}
			} else {
				a.memberRoles = map[string]string{"200": role}
			}
			job := a.target(t, "membership-"+role, "group", "123@g.us")
			present := role == "member" || role == "administrator" || role == "creator"
			if got := len(a.inspection(t, job).Missing); present && got != 0 || !present && got != 1 {
				t.Fatal(role, got)
			}
		})
	}
}

func TestMetadataReportCountsMutationsButNotPhotoIDReads(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	job := a.target(t, "counts", "group", "123@g.us")
	if r := a.report(t, metadata.ReportRequest{RunID: "counts", TargetJobs: []int64{job}}); r.State != delivery.Done {
		t.Fatal(r)
	}
	call := a.f.tgCalls[len(a.f.tgCalls)-1]
	if !strings.Contains(call.Text, "1 groups, 0 contacts, 3 fields updated.") {
		t.Fatal(call.Text)
	}
	var raw []byte
	if err := a.f.store.pool.QueryRow(t.Context(), "SELECT payload FROM metadata_reports WHERE run_id='counts'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var report metadata.Report
	if err := json.Unmarshal(raw, &report); err != nil || report.Changed != 3 || report.Targets != 1 {
		t.Fatal(report, err)
	}
}

func TestMetadataCronReportRemainsQuietWithoutWorkOrRosterChange(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	a.url = nil
	a.title = "New group"
	a.description = "New description"
	a.target(t, "warm", "group", "123@g.us")
	job := a.target(t, "quiet", "group", "123@g.us")
	if r := a.report(t, metadata.ReportRequest{RunID: "quiet", Source: "cron", TargetJobs: []int64{job}}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(a.f.tgCalls) != 0 {
		t.Fatal("quiet cron sent a report", a.f.tgCalls)
	}
	var quiet bool
	if err := a.f.store.pool.QueryRow(t.Context(), "SELECT (payload->>'Quiet')::boolean FROM metadata_reports WHERE run_id='quiet'").Scan(&quiet); err != nil || !quiet {
		t.Fatal(quiet, err)
	}
}

func TestMetadataReportFailureCannotPartiallyCommitStatsOrNotices(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	a.url = nil
	job := a.target(t, "report-rollback", "group", "123@g.us")
	if _, err := a.f.store.pool.Exec(t.Context(), `CREATE FUNCTION reject_report_plan() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.plan_key LIKE 'sync-finish:%' THEN RAISE EXCEPTION 'fixture report failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_report BEFORE INSERT ON control_plans FOR EACH ROW EXECUTE FUNCTION reject_report_plan()`); err != nil {
		t.Fatal(err)
	}
	if r := a.report(t, metadata.ReportRequest{RunID: "report-rollback", TargetJobs: []int64{job}}); r.State != delivery.Pending {
		t.Fatal(r)
	}
	var count int
	if err := a.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM metadata_reports").Scan(&count); err != nil || count != 0 || len(a.f.tgCalls) != 0 {
		t.Fatal(count, a.f.tgCalls, err)
	}
}

func TestMetadataReportUncertainTargetCannotBecomeCompletion(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	a.dropAt = 1
	job := a.enqueue(t, "uncertain-target", "group", "123@g.us", "unfinished")
	if r := a.pictureFixture.run(t, job); r.State != delivery.Uncertain {
		t.Fatal(r)
	}
	if r := a.report(t, metadata.ReportRequest{RunID: "unfinished", TargetJobs: []int64{job}}); r.State != delivery.Pending {
		t.Fatal(r)
	}
	if len(a.f.tgCalls) != 0 {
		t.Fatal("uncertain target reported complete", a.f.tgCalls)
	}
}

func TestMetadataReportForeignRunAndDuplicateTargetsAreRejected(t *testing.T) {
	for _, kind := range []string{"run", "duplicate", "foreign"} {
		t.Run(kind, func(t *testing.T) {
			a := newAdviceFixture(t)
			a.f.route(t, "123@g.us", -200, 0)
			a.url = nil
			job := a.target(t, "original", "group", "123@g.us")
			in := metadata.ReportRequest{RunID: "original", TargetJobs: []int64{job}}
			switch kind {
			case "run":
				in.RunID = "another-run"
			case "duplicate":
				in.TargetJobs = append(in.TargetJobs, job)
			case "foreign":
				in.TargetJobs = []int64{job + 9999}
			}
			if r := a.report(t, in); r.State != delivery.Failed {
				t.Fatal(r)
			}
			if len(a.f.tgCalls) != 0 {
				t.Fatal(a.f.tgCalls)
			}
		})
	}
}

func TestMetadataReportRetryKeepsOriginalCountsAndDeliveredPackets(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	a.url = nil
	job := a.target(t, "report-retry", "group", "123@g.us")
	a.f.failTG = 1
	if r := a.report(t, metadata.ReportRequest{RunID: "report-retry", TargetJobs: []int64{job}}); r.State != delivery.Pending {
		t.Fatal(r)
	}
	original := a.f.tgCalls[0]
	if _, err := a.f.store.pool.Exec(t.Context(), "UPDATE metadata_inspections SET payload=jsonb_set(payload,'{Missing}','[]')"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE dedup='sync-finish:report-retry'"); err != nil {
		t.Fatal(err)
	}
	if r := a.report(t, metadata.ReportRequest{RunID: "report-retry", TargetJobs: []int64{job}}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(a.f.tgCalls) != 2 || a.f.tgCalls[1] != original {
		t.Fatal(a.f.tgCalls)
	}
}

func TestSyncCommandsRetainScopeAndBoundContactWithoutRoute(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	a.f.bind(t, "200", "12345000000001@lid")
	for i, tc := range []struct {
		bot             string
		chat, thread    int64
		text, scope, id string
	}{
		{"master", 123, 0, "/sync", "all", ""},
		{"master", -100, 0, "/sync all", "all", ""},
		{"master", -200, 0, "/sync", "group", "123@g.us"},
		{"200", 123, 0, "/sync", "contact", "12345000000001@lid"},
	} {
		update := int64(8200 + i)
		if r := operatorCommand(t, a.f, update, tc.bot, tc.chat, tc.thread, tc.text); r.State != delivery.Done {
			t.Fatal(r)
		}
		var raw []byte
		if err := a.f.store.pool.QueryRow(t.Context(), "SELECT payload FROM jobs WHERE dedup=$1", fmt.Sprintf("management:tg:%s:%d", tc.bot, update)).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var source struct{ Data struct{ Scope, ID string } }
		if err := json.Unmarshal(raw, &source); err != nil || source.Data.Scope != tc.scope || source.Data.ID != tc.id {
			t.Fatal(string(raw), err)
		}
	}
}

func (a *adviceFixture) request(t *testing.T, key string, target *control.Target) int64 {
	t.Helper()
	data := map[string]any{"scope": "all"}
	if target != nil {
		data["notify"] = map[string]any{"bot": target.BotID, "chat": target.ChatID, "thread": target.ThreadID}
	}
	payload, _ := json.Marshal(map[string]any{"type": "sync_metadata", "data": data})
	if _, err := a.f.store.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: key, Stream: key, Kind: "management", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := a.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	// Run admission is a separate pending coordinator capability. This fixture
	// supplies its terminal admission boundary without pretending to execute it.
	if r := a.f.run(t, id, "management", func(context.Context, delivery.Job, *delivery.Effects) error { return nil }); r.State != delivery.Done {
		t.Fatal(r)
	}
	return id
}

func TestMetadataReportCoalescedRecipientsRetryOnlyUnconfirmedBot(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	a.url = nil
	a.bindBot(t, "200", "12345000000001@lid", "Alice")
	target := a.target(t, "waiters", "group", "123@g.us")
	first := a.request(t, "request-master", &control.Target{BotID: "master", ChatID: 123})
	second := a.request(t, "request-child", &control.Target{BotID: "200", ChatID: 123})
	input := metadata.ReportRequest{RunID: "waiters", TargetJobs: []int64{target}, RequestJobs: []int64{first, second, second}}
	a.f.failTG = 2
	if r := a.report(t, input); r.State != delivery.Pending {
		t.Fatal(r)
	}
	if len(a.f.tgCalls) != 2 || a.f.tgCalls[0].Bot != "master-fixture" || a.f.tgCalls[1].Bot != "child-200" {
		t.Fatal(a.f.tgCalls)
	}
	if _, err := a.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE dedup='sync-finish:waiters'"); err != nil {
		t.Fatal(err)
	}
	if r := a.report(t, input); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(a.f.tgCalls) != 3 || a.f.tgCalls[2] != a.f.tgCalls[1] {
		t.Fatal("confirmed recipient repeated", a.f.tgCalls)
	}
	var observations int
	if err := a.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM telegram_observations WHERE kind='log'").Scan(&observations); err != nil || observations != 2 {
		t.Fatal(observations, err)
	}
}

func TestMetadataQuietCronStillNotifiesManualWaiter(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	a.url = nil
	a.title = "New group"
	a.description = "New description"
	a.bindBot(t, "200", "12345000000001@lid", "Alice")
	a.target(t, "prewarm-waiter", "group", "123@g.us")
	target := a.target(t, "quiet-waiter", "group", "123@g.us")
	waiter := a.request(t, "manual-waiter", &control.Target{BotID: "200", ChatID: 123})
	if r := a.report(t, metadata.ReportRequest{RunID: "quiet-waiter", Source: "cron", TargetJobs: []int64{target}, RequestJobs: []int64{waiter}}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(a.f.tgCalls) != 1 || a.f.tgCalls[0].Bot != "child-200" || a.f.tgCalls[0].Chat != 123 {
		t.Fatal(a.f.tgCalls)
	}
}

func TestMetadataReportMissingInitialNotifierRetainsManagementAndWaiter(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	a.url = nil
	target := a.target(t, "default-notifier", "group", "123@g.us")
	initial := a.request(t, "initial-no-notify", nil)
	waiter := a.request(t, "later-owner-waiter", &control.Target{BotID: "master", ChatID: 123})
	if r := a.report(t, metadata.ReportRequest{RunID: "default-notifier", TargetJobs: []int64{target}, RequestJobs: []int64{initial, waiter}}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(a.f.tgCalls) != 2 || a.f.tgCalls[0].Chat != -100 || a.f.tgCalls[1].Chat != 123 {
		t.Fatal(a.f.tgCalls)
	}
}

func TestMetadataReportKnownFailureDoesNotHideSuccessfulContact(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	a.url = nil
	a.bindBot(t, "200", "12345000000001@lid", "Alice")
	failed := a.enqueue(t, "failed-group", "group", "123@g.us", "partial-run")
	if r := a.f.run(t, failed, "management", func(context.Context, delivery.Job, *delivery.Effects) error { return delivery.ErrUnsupported }); r.State != delivery.Failed {
		t.Fatal(r)
	}
	contact := a.target(t, "partial-run", "contact", "12345000000001@lid")
	if r := a.report(t, metadata.ReportRequest{RunID: "partial-run", TargetJobs: []int64{failed, contact}, Failures: []metadata.TargetFailure{{JobID: failed, Warning: "OpenWA group lookup failed"}}}); r.State != delivery.Done {
		t.Fatal(r)
	}
	text := a.f.tgCalls[len(a.f.tgCalls)-1].Text
	if !strings.Contains(text, "1 groups, 1 contacts, 2 fields updated.") || !strings.Contains(text, "OpenWA group lookup failed") {
		t.Fatal(text)
	}
}

func TestMetadataAdvicePlanFailureRollsBackPermissionGrant(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	a.forbid = true
	if _, err := a.f.store.pool.Exec(t.Context(), `CREATE FUNCTION reject_inspection() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture inspection failure'; END $$; CREATE TRIGGER reject_inspection BEFORE INSERT ON metadata_inspections FOR EACH ROW EXECUTE FUNCTION reject_inspection()`); err != nil {
		t.Fatal(err)
	}
	job := a.enqueue(t, "inspection-rollback", "group", "123@g.us", "inspection-rollback")
	if r := a.f.run(t, job, "management", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		if _, err := a.service.Prepare(ctx, j); err != nil {
			return err
		}
		if err := a.service.ApplyText(ctx, j, e); err != nil {
			return err
		}
		if _, err := a.pictures.Prepare(ctx, j); err != nil {
			return err
		}
		if err := a.pictures.Apply(ctx, j, e); err != nil {
			return err
		}
		_, err := a.inspector.Inspect(ctx, j)
		return err
	}); r.State != delivery.Pending {
		t.Fatal(r)
	}
	var grants int
	if err := a.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM metadata_permissions").Scan(&grants); err != nil || grants != 0 {
		t.Fatal(grants, err)
	}
}

func TestSyncCommandPlanFailureRollsBackIndependentAdmission(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	if _, err := a.f.store.pool.Exec(t.Context(), `CREATE FUNCTION reject_sync_ack() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture ack failure'; END $$; CREATE TRIGGER reject_ack BEFORE INSERT ON control_plans FOR EACH ROW EXECUTE FUNCTION reject_sync_ack()`); err != nil {
		t.Fatal(err)
	}
	if r := operatorCommand(t, a.f, 8300, "master", -200, 0, "/sync"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	var queued int
	if err := a.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE dedup='management:tg:master:8300'").Scan(&queued); err != nil || queued != 0 || len(a.f.tgCalls) != 0 {
		t.Fatal(queued, a.f.tgCalls, err)
	}
}

func TestSyncAckRetryDoesNotRequeueOrRetargetAfterRouteMove(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	a.f.failTG = 1
	if r := operatorCommand(t, a.f, 8301, "master", -200, 0, "/sync"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	if _, err := a.f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET telegram_chat_id=-201"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE dedup='tg:master:8301'"); err != nil {
		t.Fatal(err)
	}
	if r := a.f.dispatch(t, "tg:master:8301", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(a.f.tgCalls) != 2 || a.f.tgCalls[0] != a.f.tgCalls[1] {
		t.Fatal(a.f.tgCalls)
	}
	var queued int
	if err := a.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE dedup='management:tg:master:8301'").Scan(&queued); err != nil || queued != 1 {
		t.Fatal(queued, err)
	}
}

type unavailableAdvice struct{}

func (unavailableAdvice) Me(context.Context) (*models.User, error) {
	return nil, delivery.ErrUnsupported
}
func (unavailableAdvice) Chat(context.Context, *bot.GetChatParams) (*models.ChatFullInfo, error) {
	return nil, delivery.ErrUnsupported
}
func (unavailableAdvice) ChatMember(context.Context, *bot.GetChatMemberParams) (*models.ChatMember, error) {
	return nil, delivery.ErrUnsupported
}

func TestMetadataAdviceReopenedConsumerReusesFrozenGrantAndMissingBots(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	a.forbid = true
	a.bindBot(t, "200", "12345000000001@lid", "Alice")
	job := a.enqueue(t, "frozen-inspection", "group", "123@g.us", "frozen-inspection")
	if r := a.f.run(t, job, "management", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		if _, err := a.service.Prepare(ctx, j); err != nil {
			return err
		}
		if err := a.service.ApplyText(ctx, j, e); err != nil {
			return err
		}
		if _, err := a.pictures.Prepare(ctx, j); err != nil {
			return err
		}
		if err := a.pictures.Apply(ctx, j, e); err != nil {
			return err
		}
		if _, err := a.inspector.Inspect(ctx, j); err != nil {
			return err
		}
		return delivery.ErrLocalRetry
	}); r.State != delivery.Pending {
		t.Fatal(r)
	}
	before := a.inspection(t, job)
	reopened, err := Open(t.Context(), a.f.store.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, err := metadata.NewInspector(reopened, unavailableAdvice{}, a.settings)
	if err != nil {
		t.Fatal(err)
	}
	a.retry(t, job)
	if r := a.f.run(t, job, "management", func(ctx context.Context, j delivery.Job, _ *delivery.Effects) error {
		result, err := restored.Inspect(ctx, j)
		if err == nil && (result.Permission.ID != before.Permission.ID || len(result.Missing) != 1) {
			t.Error("frozen advice changed", result)
		}
		return err
	}); r.State != delivery.Done {
		t.Fatal(r)
	}
	var grants int
	if err := a.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM metadata_permissions").Scan(&grants); err != nil || grants != 1 {
		t.Fatal(grants, err)
	}
}

func TestMetadataCronRosterHashTracksRolesAndVerifiedAliases(t *testing.T) {
	for _, change := range []string{"name", "admin", "alias"} {
		t.Run(change, func(t *testing.T) {
			a := newAdviceFixture(t)
			a.f.route(t, "123@g.us", -200, 0)
			a.url = nil
			a.title = "New group"
			a.description = "New description"
			if _, err := a.f.store.pool.Exec(t.Context(), "INSERT INTO contact_aliases(account_id,alias,canonical) VALUES('fixture-account','12345000000001@lid','12025550101@c.us')"); err != nil {
				t.Fatal(err)
			}
			a.target(t, "hash-warm", "group", "123@g.us")
			participant := map[string]any{"id": "12345000000001@lid", "name": "Renamed"}
			if change == "admin" {
				participant["isAdmin"] = true
			}
			if change == "alias" {
				participant["id"] = "12025550101@c.us"
			}
			raw, _ := json.Marshal(map[string]any{"id": "123@g.us", "name": "New group", "description": "New description", "participants": []any{participant}})
			a.group = string(raw)
			target := a.target(t, "hash-change", "group", "123@g.us")
			if r := a.report(t, metadata.ReportRequest{RunID: "hash-change", Source: "cron", TargetJobs: []int64{target}}); r.State != delivery.Done {
				t.Fatal(r)
			}
			expected := 0
			if change == "admin" {
				expected = 1
			}
			if len(a.f.tgCalls) != expected {
				t.Fatal("roster hash changed on labels/aliases or ignored role change", change, a.f.tgCalls)
			}
		})
	}
}

func TestSyncInvalidScopesDoNotAdmitGlobalWork(t *testing.T) {
	for i, tc := range []struct {
		bot  string
		chat int64
		text string
	}{
		{"master", -200, "/sync all"}, {"master", -200, "/sync nonsense"}, {"200", 123, "/sync all"}, {"master", 123, "/sync nonsense"},
	} {
		a := newAdviceFixture(t)
		a.f.route(t, "123@g.us", -200, 0)
		a.f.bind(t, "200", "12345000000001@lid")
		id := int64(8400 + i)
		if r := operatorCommand(t, a.f, id, tc.bot, tc.chat, 0, tc.text); r.State != delivery.Done {
			t.Fatal(r)
		}
		var count int
		if err := a.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE dedup=$1", fmt.Sprintf("management:tg:%s:%d", tc.bot, id)).Scan(&count); err != nil || count != 0 {
			t.Fatal(tc, count, err)
		}
	}
}

type changingAdvice struct {
	metadata.AdviceTelegram
	swap    func()
	changed bool
}

func (c *changingAdvice) ChatMember(ctx context.Context, p *bot.GetChatMemberParams) (*models.ChatMember, error) {
	result, err := c.AdviceTelegram.ChatMember(ctx, p)
	if err == nil && p.UserID == 200 && !c.changed {
		c.changed = true
		c.swap()
	}
	return result, err
}
func TestMetadataAdviceBindingChangeDuringNativeReadRetriesBeforeSealing(t *testing.T) {
	a := newAdviceFixture(t)
	a.f.route(t, "123@g.us", -200, 0)
	a.url = nil
	a.bindBot(t, "200", "12345000000001@lid", "Alice")
	if err := a.f.store.PutIngressBot(t.Context(), "fixture-account", ingress.Bot{ID: "201", Username: "second_bot"}); err != nil {
		t.Fatal(err)
	}
	master, err := a.pclients(t.Context(), "fixture-account", "master")
	if err != nil {
		t.Fatal(err)
	}
	changing := &changingAdvice{AdviceTelegram: master, swap: func() {
		if _, err := a.f.store.pool.Exec(t.Context(), "UPDATE bridge_contact_bots SET bot_id='201' WHERE contact_id='12345000000001@lid'"); err != nil {
			t.Error(err)
		}
	}}
	a.inspector, err = metadata.NewInspector(a.f.store, changing, a.settings)
	if err != nil {
		t.Fatal(err)
	}
	job := a.enqueue(t, "binding-inspection", "group", "123@g.us", "binding-inspection")
	handler := func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		if _, err := a.service.Prepare(ctx, j); err != nil {
			return err
		}
		if err := a.service.ApplyText(ctx, j, e); err != nil {
			return err
		}
		if _, err := a.pictures.Prepare(ctx, j); err != nil {
			return err
		}
		if err := a.pictures.Apply(ctx, j, e); err != nil {
			return err
		}
		_, err := a.inspector.Inspect(ctx, j)
		return err
	}
	if r := a.f.run(t, job, "management", handler); r.State != delivery.Pending {
		t.Fatal(r)
	}
	var records int
	if err := a.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM metadata_inspections").Scan(&records); err != nil || records != 0 {
		t.Fatal("stale candidate set sealed", records, err)
	}
	a.retry(t, job)
	if r := a.f.run(t, job, "management", handler); r.State != delivery.Done {
		t.Fatal(r)
	}
	result := a.inspection(t, job)
	if len(result.Missing) != 1 || result.Missing[0].ID != "201" {
		t.Fatal(result)
	}
}
