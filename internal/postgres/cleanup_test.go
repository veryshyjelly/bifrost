package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/relay"
	"github.com/veryshyjelly/bifrost/migrations"
)

func cleanupRow(t *testing.T, s *Store, scope, bot string, chat, thread, message int64, kind string, date, expires int64) int64 {
	t.Helper()
	var seq int64
	err := s.pool.QueryRow(t.Context(), "INSERT INTO telegram_observations(account_id,scope,bot_id,chat_id,thread_id,message_id,kind,message_date,expires_at,payload) VALUES('fixture-account',$1,$2,$3,$4,$5,$6,$7,$8,'{}') RETURNING seq", scope, bot, chat, thread, message, kind, date, expires).Scan(&seq)
	if err != nil {
		t.Fatal(err)
	}
	return seq
}
func scheduleExpiry(t *testing.T, f *relayFixture, scope, bot string, chat, message int64) string {
	t.Helper()
	key := fmt.Sprintf("control-expire:%s:%d:%d", scope, chat, message)
	payload, _ := json.Marshal(map[string]any{"scope": scope, "bot": bot, "chat": chat, "message": message})
	if _, err := f.store.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: key, Stream: key, Kind: "control-expire", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	return key
}
func clearCommand(t *testing.T, f *relayFixture, id int64, bot string, chat, thread int64, text string) delivery.Report {
	t.Helper()
	m := pickerMessage(int(id)+10000, chat, thread, text)
	m.Date = int(time.Now().Unix())
	if !admitPicker(t, f, bot, models.Update{ID: id, Message: m}) {
		t.Fatal("clear command not admitted")
	}
	return f.dispatch(t, fmt.Sprintf("tg:%s:%d", bot, id), "tg")
}
func onlyClear(t *testing.T, f *relayFixture) control.ClearRun {
	t.Helper()
	var payload []byte
	if err := f.store.pool.QueryRow(t.Context(), "SELECT payload FROM control_clears").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var run control.ClearRun
	if err := json.Unmarshal(payload, &run); err != nil {
		t.Fatal(err)
	}
	return run
}
func nextClear(t *testing.T, f *relayFixture) string {
	t.Helper()
	var key string
	if err := f.store.pool.QueryRow(t.Context(), "SELECT dedup FROM jobs WHERE kind='control-clear' AND state='pending' ORDER BY id LIMIT 1").Scan(&key); err != nil {
		t.Fatal(err)
	}
	return key
}
func drainClear(t *testing.T, f *relayFixture) control.ClearRun {
	t.Helper()
	for i := 0; i < 300; i++ {
		run := onlyClear(t, f)
		if run.Status == "completed" || run.Status == "failed" {
			return run
		}
		key := nextClear(t, f)
		if r := f.dispatch(t, key, "control-clear"); r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	t.Fatal("clear did not finish")
	return control.ClearRun{}
}
func rowDeleted(t *testing.T, s *Store, scope string, chat, message int64) bool {
	t.Helper()
	var deleted bool
	if err := s.pool.QueryRow(t.Context(), "SELECT deleted FROM telegram_observations WHERE scope=$1 AND chat_id=$2 AND message_id=$3", scope, chat, message).Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	return deleted
}

func TestNoticeExpirySurvivesReopenAndCannotDeleteChatsOrProtectedMessages(t *testing.T) {
	f := pickerFixture(t, -200, 0)
	if r := sendPickerCommand(t, f, 1, "master", -200, 0, "/mention"); r.State != delivery.Done {
		t.Fatal(r)
	}
	var queued int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='control-expire'").Scan(&queued); err != nil || queued != 2 {
		t.Fatal("command/reply expiry not scheduled", queued, err)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE telegram_observations SET expires_at=$1 WHERE kind IN ('command','log')", time.Now().Add(-time.Second).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE kind='control-expire'"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), f.store.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	f.store.Close()
	f.store = reopened
	replies, err := control.New(reopened, func(context.Context, string, string) (control.Telegram, error) {
		t.Error("expiry used a reply client")
		return nil, delivery.ErrInvalid
	}, -100)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := control.NewCleaner(reopened, replies, f.deleteClients, control.CleanupOptions{OwnerID: 123})
	if err != nil {
		t.Fatal(err)
	}
	runExpiry := func(key string) delivery.Report {
		var id int64
		if err := reopened.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", key).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return f.run(t, id, "control-expire", restarted.Expire)
	}
	ids := []string{"control-expire:*:-200:101", "control-expire:*:-200:1001"}
	for _, key := range ids {
		r := runExpiry(key)
		if r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	if !rowDeleted(t, reopened, "*", -200, 101) || !rowDeleted(t, reopened, "*", -200, 1001) {
		t.Fatal("reopened expiry state lost")
	}
	for i, kind := range []string{"chat", "protected", "service"} {
		cleanupRow(t, f.store, "*", "master", -200, 0, int64(20+i), kind, time.Now().UnixMilli(), time.Now().Add(-time.Second).UnixMilli())
		key := scheduleExpiry(t, f, "*", "master", -200, int64(20+i))
		if r := runExpiry(key); r.State != delivery.Done {
			t.Fatal(r)
		}
		if rowDeleted(t, f.store, "*", -200, int64(20+i)) {
			t.Fatal("expiry deleted", kind)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.deleteCalls) != 2 || len(f.waCalls) != 0 || f.reads.Load() != 0 {
		t.Fatal(f.deleteCalls)
	}
}

func TestExpiryRetriesLostDeleteResponseWithoutUncertainSendEffects(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	now := time.Now().UnixMilli()
	cleanupRow(t, f.store, "*", "200", -200, 0, 11, "log", now, now-1)
	key := scheduleExpiry(t, f, "*", "master", -200, 11)
	f.deleteDropAt = 1
	r := f.dispatch(t, key, "control-expire")
	if r.State != delivery.Pending {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	r = f.dispatch(t, key, "control-expire")
	if r.State != delivery.Done || !rowDeleted(t, f.store, "*", -200, 11) {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.deleteCalls) != 2 || !reflect.DeepEqual(f.deleteCalls[0], f.deleteCalls[1]) || f.deleteCalls[0].Bot != "master-fixture" {
		t.Fatal(f.deleteCalls)
	}
	var uncertain int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM effects WHERE state IN ('sending','uncertain')").Scan(&uncertain); err != nil || uncertain != 0 {
		t.Fatal("delete became an uncertain send", uncertain, err)
	}
}
func TestCommittedExpiryOutcomeIsNotDeletedAgainOnJobRetry(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	now := time.Now().UnixMilli()
	cleanupRow(t, f.store, "*", "master", -200, 0, 11, "log", now, now-1)
	key := scheduleExpiry(t, f, "*", "master", -200, 11)
	var id int64
	if err := f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	r := f.run(t, id, "control-expire", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		if err := f.cleaner.Expire(ctx, j, e); err != nil {
			return err
		}
		return delivery.ErrLocalRetry
	})
	if r.State != delivery.Pending {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, key, "control-expire"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.deleteCalls) != 1 {
		t.Fatal("confirmed cleanup outcome repeated", f.deleteCalls)
	}
}
func TestExpiryRefusalIsRecordedAndPrivateMessageIDsStayScopedByBot(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	now := time.Now().UnixMilli()
	for _, bot := range []string{"200", "201"} {
		cleanupRow(t, f.store, bot, bot, 123, 0, 7, "log", now, now-1)
	}
	f.deleteStatus = 403
	key := scheduleExpiry(t, f, "200", "200", 123, 7)
	if r := f.dispatch(t, key, "control-expire"); r.State != delivery.Done {
		t.Fatal(r)
	}
	var problem string
	if err := f.store.pool.QueryRow(t.Context(), "SELECT cleanup_error FROM telegram_observations WHERE scope='200'").Scan(&problem); err != nil || problem != "Telegram could not remove this notice" {
		t.Fatal(problem, err)
	}
	if rowDeleted(t, f.store, "200", 123, 7) || rowDeleted(t, f.store, "201", 123, 7) {
		t.Fatal("refusal marked a private message deleted")
	}
	f.deleteStatus = 0
	key = scheduleExpiry(t, f, "201", "201", 123, 7)
	if r := f.dispatch(t, key, "control-expire"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if !rowDeleted(t, f.store, "201", 123, 7) || rowDeleted(t, f.store, "200", 123, 7) {
		t.Fatal("expiry crossed bot scope")
	}
}

func TestClearLogKeepsConversationMappingsAndLaterNotices(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.route(t, mentionGroup, -200, 0)
	if r := f.incoming(t, "ow:archive", relay.Incoming{ChatID: mentionGroup, MessageID: "archive", SenderID: mentionAlice, Name: "Alice", Body: "Keep me"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	now := time.Now().UnixMilli()
	cleanupRow(t, f.store, "*", "200", -200, 0, 11, "log", now, 0)
	cleanupRow(t, f.store, "*", "master", -200, 0, 12, "command", now, 0)
	if r := clearCommand(t, f, 1, "master", -200, 0, "/clear log"); r.State != delivery.Done {
		t.Fatal(r)
	}
	cleanupRow(t, f.store, "*", "master", -200, 0, 13, "log", now, 0)
	run := drainClear(t, f)
	if run.Removed != 3 || run.Skipped != 0 || run.Blocked != 0 {
		t.Fatal(run)
	}
	if !rowDeleted(t, f.store, "*", -200, 11) || !rowDeleted(t, f.store, "*", -200, 12) || rowDeleted(t, f.store, "*", -200, 13) || rowDeleted(t, f.store, "*", -200, 1001) {
		t.Fatal("notice clear changed chat/later records")
	}
	var mappings int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_mappings").Scan(&mappings); err != nil || mappings != 1 {
		t.Fatal("clear erased mappings", mappings, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.deleteCalls) != 1 || f.deleteCalls[0].Bot != "master-fixture" {
		t.Fatal(f.deleteCalls)
	}
}
func TestClearAllFixedSnapshotExcludesLateCommitAndOtherTopics(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.route(t, mentionGroup, -100, 88)
	now := time.Now().UnixMilli()
	tx, err := f.store.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if _, err := tx.Exec(t.Context(), "INSERT INTO telegram_observations(account_id,scope,bot_id,chat_id,thread_id,message_id,kind,message_date,payload) VALUES('fixture-account','*','master',-100,88,10,'chat',$1,'{}')", now); err != nil {
		t.Fatal(err)
	}
	cleanupRow(t, f.store, "*", "master", -100, 88, 11, "chat", now, 0)
	cleanupRow(t, f.store, "*", "master", -100, 89, 12, "chat", now, 0)
	if r := clearCommand(t, f, 1, "master", -100, 88, "/clear_all"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	cleanupRow(t, f.store, "*", "master", -100, 88, 13, "chat", now, 0)
	run := drainClear(t, f)
	if run.Removed != 2 || !rowDeleted(t, f.store, "*", -100, 11) || rowDeleted(t, f.store, "*", -100, 10) || rowDeleted(t, f.store, "*", -100, 12) || rowDeleted(t, f.store, "*", -100, 13) {
		t.Fatal("snapshot grew or crossed topic", run)
	}
}
func TestClearAllBulkRefusalFallsBackAndContinuesPastProtectedOldAndBlockedRows(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	now := time.Now().UnixMilli()
	for _, row := range []struct {
		id   int64
		kind string
		date int64
	}{{11, "log", now}, {12, "log", now}, {13, "protected", now}, {14, "chat", now - int64(49*time.Hour/time.Millisecond)}, {15, "chat", now}} {
		cleanupRow(t, f.store, "*", "master", -100, 0, row.id, row.kind, row.date, 0)
	}
	f.deleteRefuse = map[int]int{12: 400}
	if r := clearCommand(t, f, 1, "master", -100, 0, "/clear all"); r.State != delivery.Done {
		t.Fatal(r)
	}
	run := drainClear(t, f)
	if run.Removed != 3 || run.Skipped != 2 || run.Blocked != 1 || !run.Single {
		t.Fatal(run)
	}
	if !rowDeleted(t, f.store, "*", -100, 11) || rowDeleted(t, f.store, "*", -100, 12) || rowDeleted(t, f.store, "*", -100, 13) || rowDeleted(t, f.store, "*", -100, 14) || !rowDeleted(t, f.store, "*", -100, 15) {
		t.Fatal("wrong all-clear outcomes")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.deleteCalls) != 5 || len(f.deleteCalls[0].IDs) != 4 || !strings.Contains(f.tgCalls[len(f.tgCalls)-1].Text, "Cleared 3 recorded Telegram messages. 2 too old or protected; 1 could not be removed.") {
		t.Fatal(f.deleteCalls, f.tgCalls)
	}
}
func TestForbiddenClearNoticeRetriesWithoutRepeatingDeletion(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	cleanupRow(t, f.store, "*", "master", -100, 0, 11, "log", time.Now().UnixMilli(), 0)
	if r := clearCommand(t, f, 1, "master", -100, 0, "/clear"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.deleteStatus = 403
	f.failTG = 2
	key := nextClear(t, f)
	r := f.dispatch(t, key, "control-clear")
	if r.State != delivery.Pending {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	f.deleteStatus = 0
	r = f.dispatch(t, key, "control-clear")
	if r.State != delivery.Done || onlyClear(t, f).Status != "failed" {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.deleteCalls) != 1 || len(f.tgCalls) != 3 || f.tgCalls[1] != f.tgCalls[2] || !strings.Contains(f.tgCalls[2].Text, "Give Bifrost Delete Messages permission") {
		t.Fatal(f.deleteCalls, f.tgCalls)
	}
}

func TestExpiryBackfillRetainsDeadlinesAndDoesNotExpireArchives(t *testing.T) {
	s := testStore(t)
	now := time.Now().UnixMilli()
	cleanupRow(t, s, "*", "master", -200, 0, 1, "log", now, now+60000)
	cleanupRow(t, s, "*", "master", -200, 0, 2, "chat", now, now+60000)
	cleanupRow(t, s, "*", "master", -200, 0, 3, "protected", now, now+60000)
	cleanupRow(t, s, "*", "master", -200, 0, 4, "log", now, 0)
	data, err := migrations.Files.ReadFile("00009_cleanup.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "INSERT INTO jobs(")
	end := strings.Index(text[start:], ";") + start + 1
	for i := 0; i < 2; i++ {
		if _, err := s.pool.Exec(t.Context(), text[start:end]); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var deadline int64
	if err := s.pool.QueryRow(t.Context(), "SELECT count(*),max((extract(epoch FROM due)*1000)::bigint) FROM jobs WHERE kind='control-expire'").Scan(&count, &deadline); err != nil || count != 1 || deadline != now+60000 {
		t.Fatal(count, deadline, err)
	}
}

func TestExpiryLocalCommitFailureRetriesTheSameIdempotentTarget(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	now := time.Now().UnixMilli()
	cleanupRow(t, f.store, "*", "master", -200, 0, 11, "log", now, now-1)
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE cleanup_steps ADD CONSTRAINT fixture_commit_failure CHECK (payload->>'Outcome'='pending')"); err != nil {
		t.Fatal(err)
	}
	key := scheduleExpiry(t, f, "*", "master", -200, 11)
	r := f.dispatch(t, key, "control-expire")
	if r.State != delivery.Pending || rowDeleted(t, f.store, "*", -200, 11) {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE cleanup_steps DROP CONSTRAINT fixture_commit_failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, key, "control-expire"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.deleteCalls) != 2 || !reflect.DeepEqual(f.deleteCalls[0], f.deleteCalls[1]) || !rowDeleted(t, f.store, "*", -200, 11) {
		t.Fatal(f.deleteCalls)
	}
}
func TestClearAllRetainsMappingsAndPreventsQuotesToDeletedCopies(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.route(t, mentionGroup, -100, 88)
	if r := f.incoming(t, "ow:mapped", relay.Incoming{ChatID: mentionGroup, MessageID: "mapped", SenderID: mentionAlice, Name: "Alice", Body: "Clear this copy"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE telegram_observations SET message_date=$1 WHERE message_id=1001", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if r := clearCommand(t, f, 1, "master", -100, 88, "/clear_all"); r.State != delivery.Done {
		t.Fatal(r)
	}
	run := drainClear(t, f)
	if run.Removed != 2 || !rowDeleted(t, f.store, "*", -100, 1001) {
		t.Fatal(run)
	}
	var count int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_mappings").Scan(&count); err != nil || count != 1 {
		t.Fatal("mapping was erased", count, err)
	}
	if _, found, err := f.store.WhatsAppQuote(t.Context(), "fixture-account", mentionGroup, "mapped", "master", -100, 88); err != nil || found {
		t.Fatal("deleted Telegram copy remained a quote", found, err)
	}
}
func TestClearAllRejectsMirrorPrivateAndMemberBotRequests(t *testing.T) {
	for _, tc := range []struct {
		name, bot, text string
		chat, thread    int64
	}{{"mirror", "master", "/clear all", -200, 0}, {"private", "master", "/clear_all", 123, 0}, {"member", "200", "/clear_all@alice_bot", -100, 88}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRelayFixture(t, nil, true)
			f.route(t, mentionGroup, tc.chat, tc.thread)
			if tc.bot != "master" {
				if err := f.store.PutIngressBot(t.Context(), "fixture-account", ingress.Bot{ID: tc.bot, Username: "alice_bot"}); err != nil {
					t.Fatal(err)
				}
			}
			if r := clearCommand(t, f, 1, tc.bot, tc.chat, tc.thread, tc.text); r.State != delivery.Done {
				t.Fatal(r)
			}
			var count int
			if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM control_clears").Scan(&count); err != nil || count != 0 {
				t.Fatal("forbidden all-clear was queued", count, err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.deleteCalls) != 0 || len(f.tgCalls) != 1 || !strings.Contains(f.tgCalls[0].Text, "available only in the management group") {
				t.Fatal(f.deleteCalls, f.tgCalls)
			}
		})
	}
}
func TestPrivateNoticeClearDoesNotCrossBotScope(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	now := time.Now().UnixMilli()
	for _, bot := range []string{"200", "201"} {
		if err := f.store.PutIngressBot(t.Context(), "fixture-account", ingress.Bot{ID: bot}); err != nil {
			t.Fatal(err)
		}
		cleanupRow(t, f.store, bot, bot, 123, 0, 7, "log", now, 0)
	}
	if r := clearCommand(t, f, 1, "200", 123, 0, "/clear_log"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if run := drainClear(t, f); run.Removed != 2 {
		t.Fatal(run)
	}
	if !rowDeleted(t, f.store, "200", 123, 7) || rowDeleted(t, f.store, "201", 123, 7) {
		t.Fatal("private cleanup crossed bot scope")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.deleteCalls) != 1 || f.deleteCalls[0].Bot != "child-200" {
		t.Fatal(f.deleteCalls)
	}
}

func TestNoticeClearWaitsForUnresolvedRelayAndProtectsReceiptBeforeMapping(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.route(t, mentionGroup, -200, 0)
	event := sourceEvent(t, "in-flight", mentionGroup, nil)
	if _, err := f.store.Enqueue(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	source := claim(t, f.store)
	route, _, err := f.store.ConversationRoute(t.Context(), source.AccountID, mentionGroup)
	if err != nil {
		t.Fatal(err)
	}
	p := relay.Plan{Key: relay.IncomingKey(mentionGroup, "in-flight"), Prefix: "ow:in-flight:to:master:-200", Direction: "in", WhatsAppChat: mentionGroup, WhatsAppMessage: "in-flight", BotID: "master", TelegramChat: -200, Route: route, Parts: []relay.TelegramRequest{{BotID: "master", ChatID: -200, Text: "Archive"}}}
	p, err = f.store.PrepareRelay(t.Context(), source, p)
	if err != nil {
		t.Fatal(err)
	}
	key, request, err := p.FrozenRequest(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.BeginEffect(t.Context(), source, key, request); err != nil {
		t.Fatal(err)
	}
	cleanupRow(t, f.store, "*", "master", -200, 0, 11, "log", time.Now().UnixMilli(), 0)
	if r := clearCommand(t, f, 1, "master", -200, 0, "/clear log"); r.State != delivery.Done {
		t.Fatal(r)
	}
	clearKey := nextClear(t, f)
	r := f.dispatch(t, clearKey, "control-clear")
	if r.State != delivery.Pending {
		t.Fatal("cleanup did not wait for an unresolved relay", r)
	}
	f.mu.Lock()
	deleted := len(f.deleteCalls)
	f.mu.Unlock()
	if deleted != 0 {
		t.Fatal("unknown relay copy was deleted")
	}
	receipt := json.RawMessage(`{"message_id":11,"date":1700000000,"chat":{"id":-200,"type":"supergroup"}}`)
	if err := f.store.ResolveEffect(t.Context(), source, key, delivery.Done, receipt); err != nil {
		t.Fatal(err)
	}
	finish(t, f.store, source)
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, clearKey, "control-clear"); r.State != delivery.Done {
		t.Fatal(r)
	}
	run := drainClear(t, f)
	if run.Removed != 1 || rowDeleted(t, f.store, "*", -200, 11) {
		t.Fatal("receipt-only conversation copy was cleared", run)
	}
}
