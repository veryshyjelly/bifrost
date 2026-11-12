package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/bridge"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/operations"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

type operationFixture struct {
	n *nativeFixture
	s *operations.Service
	c operations.Settings
}

func TestOperationsNoticeInsertFailureRollsBackSourceState(t *testing.T) {
	f := newOperationFixture(t)
	if err := f.n.f.store.ConfigureOperationNotices(t.Context(), "fixture-account", f.c); err != nil {
		t.Fatal(err)
	}
	id := f.n.f.enqueue(t, "wa", "rollback-source", "source:rollback", "")
	j, err := f.n.f.store.Claim(t.Context(), 30*time.Second)
	if err != nil || j.ID != id {
		t.Fatal(j, err)
	}
	ddl := `CREATE FUNCTION reject_operation_notice() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='management' AND NEW.payload->>'type'='delivery_problem' THEN RAISE EXCEPTION 'fixture notice failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_operation_notice BEFORE INSERT ON jobs FOR EACH ROW EXECUTE FUNCTION reject_operation_notice()`
	if _, err := f.n.f.store.pool.Exec(t.Context(), ddl); err != nil {
		t.Fatal(err)
	}
	if err := f.n.f.store.SetJobState(t.Context(), j, delivery.Failed, 0); err == nil {
		t.Fatal("failed outbox insert accepted source transition")
	}
	var state string
	if err := f.n.f.store.pool.QueryRow(t.Context(), "SELECT state FROM jobs WHERE id=$1", id).Scan(&state); err != nil || state != "running" {
		t.Fatal(state, err)
	}
	if _, err := f.n.f.store.pool.Exec(t.Context(), "DROP TRIGGER reject_operation_notice ON jobs; DROP FUNCTION reject_operation_notice()"); err != nil {
		t.Fatal(err)
	}
	if err := f.n.f.store.SetJobState(t.Context(), j, delivery.Failed, 0); err != nil {
		t.Fatal(err)
	}
	var notices int
	if err := f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE payload->>'type'='delivery_problem'").Scan(&notices); err != nil || notices != 1 {
		t.Fatal(notices, err)
	}
}

func TestOperationsStatusKeepsLegacyLatestSyncShape(t *testing.T) {
	f := newOperationFixture(t)
	job := f.n.f.enqueue(t, "control-sync-report", "completed-report", "report:fixture", "")
	if _, err := f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET state='done' WHERE id=$1", job); err != nil {
		t.Fatal(err)
	}
	if err := f.n.f.store.queries.CreateMetadataRun(t.Context(), db.CreateMetadataRunParams{AccountID: "fixture-account", RunID: "last-run", SessionID: "fixture-session", Scope: "all", Source: "command"}); err != nil {
		t.Fatal(err)
	}
	if err := f.n.f.store.queries.SetMetadataRunState(t.Context(), db.SetMetadataRunStateParams{AccountID: "fixture-account", RunID: "last-run", State: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := f.n.f.store.queries.CreateMetadataReport(t.Context(), db.CreateMetadataReportParams{AccountID: "fixture-account", RunID: "last-run", JobID: job, Payload: []byte(`{"RunID":"last-run","Source":"command","FinishedAt":1700000000000,"Targets":3,"Groups":1,"Contacts":2,"Changed":4,"Warnings":1,"MissingBots":2,"SkippedPhotos":3}`)}); err != nil {
		t.Fatal(err)
	}
	if r := f.command(t, 504, "/status", 123, 0); r.State != delivery.Done {
		t.Fatal(r)
	}
	p, found, err := f.n.f.store.ControlPlan(t.Context(), "fixture-account", "tg:master:504")
	if err != nil || !found {
		t.Fatal(p, err)
	}
	var status operations.Status
	if json.Unmarshal([]byte(p.Packets[0].Text), &status) != nil || status.MetadataSync.Last == nil || status.MetadataSync.Last.Run != "last-run" || status.MetadataSync.Last.Changed != 4 || status.MetadataSync.Last.SkippedPhotos != 3 || !strings.Contains(p.Packets[0].Text, `"finishedAt"`) {
		t.Fatal(status, p.Packets)
	}
}

func newOperationFixture(t *testing.T) *operationFixture {
	t.Helper()
	n := newNativeFixture(t, nil)
	replies, err := control.New(n.f.store, func(_ context.Context, _ string, id string) (control.Telegram, error) { return n.tgClients[id], nil }, -100)
	if err != nil {
		t.Fatal(err)
	}
	c := operations.Settings{Session: "fixture-session", OwnerID: 123, Management: -100, CommandTTL: 30, ControlTTL: 600, NoticeTTL: 90, Configured: true, SyncCron: "0 2 * * *", SyncTimezone: "UTC"}
	s, err := operations.New(n.f.store, replies, func(_ context.Context, _ string, id string) (operations.Telegram, error) { return n.tgClients[id], nil }, c)
	if err != nil {
		t.Fatal(err)
	}
	options := n.f.dispatchOptions
	previous := options.Extensions.OwnerCommands
	options.Extensions.OwnerCommands = func(ctx context.Context, j delivery.Job, e *delivery.Effects, id string, u *models.Update) error {
		if err := s.Handle(ctx, j, e, id, u); !errors.Is(err, delivery.ErrUnsupported) {
			return err
		}
		if previous != nil {
			return previous(ctx, j, e, id, u)
		}
		return delivery.ErrUnsupported
	}
	options.Extensions.WhatsApp = map[sdk.WebhookEvent]delivery.Handler{sdk.EventSessionStatus: s.SessionEvent, sdk.EventSessionDisconnected: s.SessionEvent, sdk.EventMessageFailed: s.SessionEvent}
	options.Extensions.Management["session_status"], options.Extensions.Management["delivery_problem"] = s.Management, s.Management
	n.f.dispatcher, err = bridge.New(n.f.store, n.f.service, n.f.whatsapp, options)
	if err != nil {
		t.Fatal(err)
	}
	return &operationFixture{n: n, s: s, c: c}
}
func (f *operationFixture) command(t *testing.T, update int64, text string, chat, thread int64) delivery.Report {
	t.Helper()
	u := models.Update{ID: update, Message: pickerMessage(int(update), chat, thread, text)}
	if !admitPicker(t, f.n.f, "master", u) {
		t.Fatal("command not admitted")
	}
	return f.n.f.dispatch(t, "tg:master:"+strconv.FormatInt(update, 10), "tg")
}

func TestOperationsStatusFreezesSafeSnapshotAndCommandExpiry(t *testing.T) {
	f := newOperationFixture(t)
	f.n.f.bind(t, "200", "12345000000001@lid")
	f.n.f.route(t, "peer@g.us", -200, 0)
	f.n.failMethod, f.n.failCode = "sendMessage", 429
	first := f.command(t, 501, "/status", 123, 0)
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	p, found, err := f.n.f.store.ControlPlan(t.Context(), "fixture-account", "tg:master:501")
	if err != nil || !found || len(p.Packets) != 1 {
		t.Fatal(p, found, err)
	}
	var status operations.Status
	if json.Unmarshal([]byte(p.Packets[0].Text), &status) != nil || !status.Configured || status.Bots != 1 || status.Conversations != 1 || status.MetadataSync.Timezone != "UTC" || status.MetadataSync.Last != nil || status.TelegramControls.CommandTTLSeconds != 30 || len(status.Jobs) == 0 {
		t.Fatal(status, p.Packets)
	}
	for _, private := range []string{"fixture-session", "fixture-key", "fixture-token", "EncryptionKey", "DATABASE_URL"} {
		if strings.Contains(p.Packets[0].Text, private) {
			t.Fatal("private configuration reached status")
		}
	}
	f.n.f.bind(t, "201", "2@s.whatsapp.net")
	f.n.mu.Lock()
	f.n.failMethod = ""
	f.n.mu.Unlock()
	if _, err := f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", first.JobID); err != nil {
		t.Fatal(err)
	}
	if r := f.n.f.dispatch(t, "tg:master:501", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.mu.Lock()
	defer f.n.mu.Unlock()
	if len(f.n.calls) != 2 || f.n.calls[0].Text != f.n.calls[1].Text {
		t.Fatal("snapshot changed on reply retry", f.n.calls)
	}
	var expiry int64
	if err := f.n.f.store.pool.QueryRow(t.Context(), "SELECT expires_at FROM telegram_observations WHERE scope='master' AND chat_id=123 AND message_id=501").Scan(&expiry); err != nil || expiry <= 0 {
		t.Fatal(expiry, err)
	}
}

func TestOperationsInboxFreezesNativeForumReadAcrossNoticeRetry(t *testing.T) {
	f := newOperationFixture(t)
	f.n.failMethod, f.n.failCode = "sendMessage", 429
	first := f.command(t, 502, "/inbox", -100, 44)
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	f.n.mu.Lock()
	f.n.failMethod = ""
	f.n.forum = false
	f.n.mu.Unlock()
	if _, err := f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", first.JobID); err != nil {
		t.Fatal(err)
	}
	if r := f.n.f.dispatch(t, "tg:master:502", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.mu.Lock()
	defer f.n.mu.Unlock()
	if len(f.n.calls) != 3 || f.n.calls[0].Method != "getChat" || f.n.calls[1].Text != f.n.calls[2].Text || !strings.Contains(f.n.calls[2].Text, "Management inbox connected") {
		t.Fatal(f.n.calls)
	}
}

func TestOperationsInboxGuidesWrongManagementWithoutChangingConfiguration(t *testing.T) {
	f := newOperationFixture(t)
	if r := f.command(t, 503, "/inbox", -200, 0); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.mu.Lock()
	defer f.n.mu.Unlock()
	if len(f.n.calls) != 2 || !strings.Contains(f.n.calls[1].Text, "TELEGRAM_MANAGEMENT_CHAT_ID=-200") {
		t.Fatal(f.n.calls)
	}
	var routes int
	if err := f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM bridge_routes").Scan(&routes); err != nil || routes != 0 {
		t.Fatal(routes, err)
	}
}

func TestOperationsSessionEventsQueueIndependentLiteralNotices(t *testing.T) {
	f := newOperationFixture(t)
	raw := []byte(`{"event":"session.status","sessionId":"fixture-session","idempotencyKey":"session-event","data":{"status":"<connected>"}}`)
	if !fixtureIngress(t, f.n.f)("openwa", raw) {
		t.Fatal("event not admitted")
	}
	if r := f.n.f.dispatch(t, "ow:session-event", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.n.f.dispatch(t, "management:ow:session-event", "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.mu.Lock()
	defer f.n.mu.Unlock()
	if len(f.n.calls) != 1 || f.n.calls[0].Chat != -100 || f.n.calls[0].Thread != 0 || f.n.calls[0].Text != "OpenWA: session.status · <connected>" || len(f.n.sends) != 0 {
		t.Fatal(f.n.calls, f.n.sends)
	}
	var routes int
	if err := f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM bridge_routes").Scan(&routes); err != nil || routes != 0 {
		t.Fatal(routes, err)
	}
}

func TestOperationsFailureNoticeAdmissionIsAtomicAndDoesNotRecurse(t *testing.T) {
	f := newOperationFixture(t)
	if err := f.n.f.store.ConfigureOperationNotices(t.Context(), "fixture-account", f.c); err != nil {
		t.Fatal(err)
	}
	id := f.n.f.enqueue(t, "wa", "failing-source", "source:failing", "")
	if r := f.n.f.run(t, id, "wa", func(context.Context, delivery.Job, *delivery.Effects) error { return delivery.ErrInvalid }); r.State != delivery.Failed {
		t.Fatal(r)
	}
	var notice int64
	var key string
	if err := f.n.f.store.pool.QueryRow(t.Context(), "SELECT id,dedup FROM jobs WHERE kind='management' AND payload->>'type'='delivery_problem'").Scan(&notice, &key); err != nil {
		t.Fatal(err)
	}
	f.n.failMethod, f.n.failCode = "sendMessage", 403
	if r := f.n.f.run(t, notice, "management", f.s.Management); r.State != delivery.Failed {
		t.Fatal(r)
	}
	var notices int
	if err := f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE payload->>'type'='delivery_problem'").Scan(&notices); err != nil || notices != 1 {
		t.Fatal(notices, err)
	}
	f.n.mu.Lock()
	defer f.n.mu.Unlock()
	if len(f.n.calls) != 1 || !strings.Contains(f.n.calls[0].Text, "definitive failure") || !strings.Contains(f.n.calls[0].Text, "local bridge CLI") {
		t.Fatal(key, f.n.calls)
	}
}

func TestOperationsDiagnosticStoreDoesNotAdmitFailureNotices(t *testing.T) {
	f := newOperationFixture(t)
	id := f.n.f.enqueue(t, "wa", "diagnostic-failure", "source:diagnostic", "")
	if r := f.n.f.run(t, id, "wa", func(context.Context, delivery.Job, *delivery.Effects) error { return delivery.ErrInvalid }); r.State != delivery.Failed {
		t.Fatal(r)
	}
	var notices int
	if err := f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE payload->>'type'='delivery_problem'").Scan(&notices); err != nil || notices != 0 {
		t.Fatal(notices, err)
	}
}

func TestOperationsRecoveryAdmitsReviewNoticeWithoutReplayingUnknownEffect(t *testing.T) {
	f := newOperationFixture(t)
	if err := f.n.f.store.ConfigureOperationNotices(t.Context(), "fixture-account", f.c); err != nil {
		t.Fatal(err)
	}
	id := f.n.f.enqueue(t, "wa", "crashed-source", "source:crashed", "")
	j, err := f.n.f.store.Claim(t.Context(), 30*time.Second)
	if err != nil || j.ID != id {
		t.Fatal(j, err)
	}
	if _, err := f.n.f.store.BeginEffect(t.Context(), j, "crashed-effect", json.RawMessage(`{"body":"frozen"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	if n, err := f.n.f.store.RecoverExpired(t.Context()); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var notices, uncertain int
	if err := f.n.f.store.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM jobs WHERE payload->>'type'='delivery_problem'),(SELECT count(*) FROM effects WHERE state='uncertain')").Scan(&notices, &uncertain); err != nil || notices != 1 || uncertain != 1 {
		t.Fatal(notices, uncertain, err)
	}
	if n, err := f.n.f.store.RecoverExpired(t.Context()); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
