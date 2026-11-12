package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/attachments"
	"github.com/veryshyjelly/bifrost/internal/bridge"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/recovery"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

type beginRepairFailure struct {
	*Store
	fail bool
}

func (s *beginRepairFailure) BeginNativeRepair(ctx context.Context, j delivery.Job, key string, r relay.NativeRepair) (relay.NativePlan, error) {
	p, err := s.Store.BeginNativeRepair(ctx, j, key, r)
	if err == nil && s.fail {
		s.fail = false
		return p, delivery.ErrLocalRetry
	}
	return p, err
}
func topicReviewNative(t *testing.T, n *nativeFixture, store relay.NativeStore) {
	t.Helper()
	sources, err := attachments.NewSources(n.assets, n.waClient, func(_ context.Context, _ string, id string) (attachments.Telegram, error) {
		return n.tgClients[id], nil
	}, "fixture-session", t.TempDir(), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := attachments.NewCodec(n.assets, attachments.CodecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	service, err := relay.NewNative(store, n.f.routing, func(_ context.Context, _ string, id string) (relay.NativeTelegram, error) {
		return n.tgClients[id], nil
	}, n.assets, sources, codec, relay.NativeOptions{Session: "fixture-session", OwnerID: 123, Management: -100, MaxBytes: 8 << 20, Identity: n.f.whatsapp, Mentions: n.f.mentionService, TextResume: n.f.service})
	if err != nil {
		t.Fatal(err)
	}
	options := n.f.dispatchOptions
	options.Extensions.Incoming = service.Incoming
	n.f.dispatcher, err = bridge.New(n.f.store, n.f.service, n.f.whatsapp, options)
	if err != nil {
		t.Fatal(err)
	}
}
func stopTopicJob(t *testing.T, n *nativeFixture, id int64) {
	t.Helper()
	if _, err := n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	if r := n.f.run(t, id, "wa", func(context.Context, delivery.Job, *delivery.Effects) error { return delivery.ErrInvalid }); r.State != delivery.Failed {
		t.Fatal(r)
	}
}

func TestTopicReviewResumesGuardWhoseEffectWasNeverPrepared(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	n.failMethod, n.failCode, n.failDescription, n.failThread = "sendPhoto", 400, "Bad Request: message thread not found", 44
	topicReviewNative(t, n, &beginRepairFailure{Store: n.f.store, fail: true})
	first := n.incoming(t, "review-unprepared", "Original")
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	stopTopicJob(t, n, first.JobID)
	snapshot, err := n.f.store.InspectTopic(t.Context(), first.JobID)
	if err != nil || snapshot.EffectState != "unprepared" || snapshot.Blocked != "" {
		t.Fatal(snapshot, err)
	}
	if _, err := n.f.store.ReconcileTopic(t.Context(), topicDecision(snapshot, "resume-unprepared", "absent", 0)); err != nil {
		t.Fatal(err)
	}
	if r := n.f.dispatch(t, "ow:review-unprepared", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 3 || n.calls[1].Method != "createForumTopic" || n.calls[2].Thread != 99 {
		t.Fatal(n.calls)
	}
}

func TestTopicReviewNeverRewritesConfirmedTopicReceipt(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	n.failMethod, n.failCode, n.failDescription, n.failThread = "sendPhoto", 400, "Bad Request: message thread not found", 44
	topicReviewNative(t, n, &nativeRepairCrash{Store: n.f.store, fail: true})
	first := n.incoming(t, "review-confirmed", "Original")
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	stopTopicJob(t, n, first.JobID)
	snapshot, err := n.f.store.InspectTopic(t.Context(), first.JobID)
	if err != nil || snapshot.EffectState != "done" {
		t.Fatal(snapshot, err)
	}
	before, err := n.f.store.queries.ReadTopicReviewEffect(t.Context(), db.ReadTopicReviewEffectParams{AccountID: "fixture-account", EffectKey: snapshot.Effect})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.f.store.ReconcileTopic(t.Context(), topicDecision(snapshot, "deny-absence", "absent", 0)); !errors.Is(err, recovery.ErrConflict) {
		t.Fatal("discarded confirmed receipt", err)
	}
	if _, err := n.f.store.ReconcileTopic(t.Context(), topicDecision(snapshot, "deny-replacement", "adopt", 100)); !errors.Is(err, recovery.ErrConflict) {
		t.Fatal("replaced confirmed receipt", err)
	}
	if _, err := n.f.store.ReconcileTopic(t.Context(), topicDecision(snapshot, "resume-confirmed", "adopt", 99)); err != nil {
		t.Fatal(err)
	}
	if r := n.f.dispatch(t, "ow:review-confirmed", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	after, err := n.f.store.queries.ReadTopicReviewEffect(t.Context(), db.ReadTopicReviewEffectParams{AccountID: "fixture-account", EffectKey: snapshot.Effect})
	if err != nil || !bytes.Equal(before.Request, after.Request) || !bytes.Equal(before.Result, after.Result) {
		t.Fatal("immutable receipt changed", err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 3 {
		t.Fatal(n.calls)
	}
}

func TestTopicReviewAuditFailureRollsBackEffectJobAndKeepsGuard(t *testing.T) {
	n, first, snapshot := lostRepairTopic(t, "review-rollback")
	ddl := `CREATE FUNCTION reject_topic_review() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture audit failure'; END $$; CREATE TRIGGER reject_topic_review BEFORE INSERT ON topic_reconciliations FOR EACH ROW EXECUTE FUNCTION reject_topic_review()`
	if _, err := n.f.store.pool.Exec(t.Context(), ddl); err != nil {
		t.Fatal(err)
	}
	d := topicDecision(snapshot, "rollback-review", "adopt", 99)
	if _, err := n.f.store.ReconcileTopic(t.Context(), d); err == nil {
		t.Fatal("audit failure committed review")
	}
	current, err := n.f.store.InspectTopic(t.Context(), first.JobID)
	if err != nil || current.State != "uncertain" || current.EffectState != "uncertain" || current.Blocked != "" {
		t.Fatal(current, err)
	}
	if _, err := n.f.store.pool.Exec(t.Context(), "DROP TRIGGER reject_topic_review ON topic_reconciliations; DROP FUNCTION reject_topic_review()"); err != nil {
		t.Fatal(err)
	}
	if _, err := n.f.store.ReconcileTopic(t.Context(), d); err != nil {
		t.Fatal(err)
	}
}

func topicDecision(s recovery.TopicSnapshot, id, action string, thread int64) recovery.TopicDecision {
	return recovery.TopicDecision{OperationID: id, AccountID: s.AccountID, Job: s.Job, Effect: s.Effect, ExpectedState: s.State, ExpectedEffectState: s.EffectState, RequestDigest: s.RequestDigest, Action: action, Management: s.Management, ThreadID: thread, ConfirmCreated: action == "adopt", ConfirmNoTopic: action == "absent"}
}
func lostRepairTopic(t *testing.T, key string) (*nativeFixture, delivery.Report, recovery.TopicSnapshot) {
	t.Helper()
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	n.failMethod, n.failCode, n.failDescription, n.failThread = "sendPhoto", 400, "Bad Request: message thread not found", 44
	n.dropMethod = "createForumTopic"
	first := n.incoming(t, key, "Original caption")
	if first.State != delivery.Uncertain {
		t.Fatal(first)
	}
	snapshot, err := n.f.store.InspectTopic(t.Context(), first.JobID)
	if err != nil || snapshot.Scope != "native-repair" || snapshot.EffectState != "uncertain" || snapshot.Blocked != "" {
		t.Fatal(snapshot, err)
	}
	return n, first, snapshot
}

func TestTopicReviewAdoptsNativeRepairAfterGenericSkipAndReplaysAuditOnly(t *testing.T) {
	n, first, _ := lostRepairTopic(t, "review-native")
	if err := n.f.store.Review(t.Context(), first.JobID, false, false); err != nil {
		t.Fatal(err)
	}
	snapshot, err := n.f.store.InspectTopic(t.Context(), first.JobID)
	if err != nil || snapshot.State != "done" {
		t.Fatal(snapshot, err)
	}
	d := topicDecision(snapshot, "adopt-native", "adopt", 99)
	result, err := n.f.store.ReconcileTopic(t.Context(), d)
	if err != nil || result.State != "pending" {
		t.Fatal(result, err)
	}
	var guards int
	if err := n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM route_transition_locks").Scan(&guards); err != nil || guards != 1 {
		t.Fatal("review released guard early", guards, err)
	}
	n.mu.Lock()
	n.dropMethod = ""
	n.mu.Unlock()
	if r := n.f.dispatch(t, "ow:review-native", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.f.route(t, "peer@g.us", -200, 0)
	if replay, err := n.f.store.ReconcileTopic(t.Context(), d); err != nil || replay != result {
		t.Fatal(replay, err)
	}
	route, _, err := n.f.store.ConversationRoute(t.Context(), "fixture-account", "peer@g.us")
	if err != nil || route.TelegramChatID != -200 {
		t.Fatal("audit replay reapplied route", route, err)
	}
	var state string
	if err := n.f.store.pool.QueryRow(t.Context(), "SELECT state FROM jobs WHERE id=$1", first.JobID).Scan(&state); err != nil || state != "done" {
		t.Fatal("audit replay reset job", state, err)
	}
	d.ThreadID = 100
	if _, err := n.f.store.ReconcileTopic(t.Context(), d); !errors.Is(err, delivery.ErrInvalid) {
		t.Fatal("operation ID accepted changed input", err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 3 || n.calls[1].Method != "createForumTopic" || n.calls[2].Thread != 99 {
		t.Fatal(n.calls)
	}
}

func TestTopicReviewConfirmedAbsenceRetriesOnlyOriginalCreation(t *testing.T) {
	n, _, snapshot := lostRepairTopic(t, "review-absent")
	d := topicDecision(snapshot, "confirm-absent", "absent", 0)
	if _, err := n.f.store.ReconcileTopic(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	n.dropMethod = ""
	n.mu.Unlock()
	if r := n.f.dispatch(t, "ow:review-absent", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 4 || n.calls[1].Method != "createForumTopic" || n.calls[2].Method != "createForumTopic" || n.calls[3].Thread != 99 {
		t.Fatal(n.calls)
	}
	effect, err := n.f.store.queries.GetEffect(t.Context(), db.GetEffectParams{AccountID: "fixture-account", EffectKey: snapshot.Effect})
	if err != nil || effect.State != "done" {
		t.Fatal(effect.State, err)
	}
}

func TestTopicReviewRejectsChangedProofOccupiedTopicsAndLiveJobLocks(t *testing.T) {
	n, first, snapshot := lostRepairTopic(t, "review-conflicts")
	d := topicDecision(snapshot, "conflict", "adopt", 99)
	bad := d
	bad.RequestDigest = strings.Repeat("0", 64)
	if _, err := n.f.store.ReconcileTopic(t.Context(), bad); !errors.Is(err, recovery.ErrConflict) {
		t.Fatal(err)
	}
	bad = d
	bad.AccountID = "foreign-account"
	if _, err := n.f.store.ReconcileTopic(t.Context(), bad); !errors.Is(err, delivery.ErrInvalid) {
		t.Fatal(err)
	}
	bad = d
	bad.Management = -200
	if _, err := n.f.store.ReconcileTopic(t.Context(), bad); !errors.Is(err, recovery.ErrConflict) {
		t.Fatal(err)
	}
	bad = d
	bad.ThreadID = 44
	if _, err := n.f.store.ReconcileTopic(t.Context(), bad); !errors.Is(err, recovery.ErrConflict) {
		t.Fatal("adopted missing old topic", err)
	}
	if err := n.f.store.queries.ReserveRetirementTopic(t.Context(), db.ReserveRetirementTopicParams{AccountID: "fixture-account", TelegramChatID: -100, ThreadID: 99, WhatsappChat: "other@g.us"}); err != nil {
		t.Fatal(err)
	}
	if _, err := n.f.store.ReconcileTopic(t.Context(), d); !errors.Is(err, recovery.ErrConflict) {
		t.Fatal("adopted reserved topic", err)
	}
	n.f.route(t, "other@g.us", -100, 88)
	bad = d
	bad.ThreadID = 88
	if _, err := n.f.store.ReconcileTopic(t.Context(), bad); !errors.Is(err, recovery.ErrConflict) {
		t.Fatal("adopted occupied topic", err)
	}
	tx, err := n.f.store.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err := n.f.store.queries.WithTx(tx).LockTopicReviewJob(t.Context(), first.JobID); err != nil {
		t.Fatal(err)
	}
	bad = d
	bad.ThreadID = 100
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := n.f.store.ReconcileTopic(ctx, bad); !errors.Is(err, recovery.ErrConflict) {
		t.Fatal("live row lock did not reject promptly", err)
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := n.f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET thread_id=55 WHERE whatsapp_chat='peer@g.us'"); err != nil {
		t.Fatal(err)
	}
	if _, err := n.f.store.ReconcileTopic(t.Context(), bad); !errors.Is(err, recovery.ErrConflict) {
		t.Fatal("stale route adopted", err)
	}
	var audits int
	if err := n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM topic_reconciliations").Scan(&audits); err != nil || audits != 0 {
		t.Fatal(audits, err)
	}
}

func TestTopicReviewNeverResolvesOtherUncertainEffects(t *testing.T) {
	n, first, snapshot := lostRepairTopic(t, "review-other-unknown")
	if err := n.f.store.queries.CreateEffect(t.Context(), db.CreateEffectParams{AccountID: "fixture-account", JobID: first.JobID, EffectKey: "unrelated-send", Request: []byte(`{"body":"frozen"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := n.f.store.pool.Exec(t.Context(), "UPDATE effects SET state='uncertain' WHERE effect_key='unrelated-send'"); err != nil {
		t.Fatal(err)
	}
	if _, err := n.f.store.ReconcileTopic(t.Context(), topicDecision(snapshot, "other-unknown", "adopt", 99)); !errors.Is(err, recovery.ErrConflict) {
		t.Fatal(err)
	}
	var uncertain int
	if err := n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM effects WHERE state='uncertain'").Scan(&uncertain); err != nil || uncertain != 2 {
		t.Fatal(uncertain, err)
	}
}

func TestTopicReviewAdoptsStatusSetupWithoutSecondCreation(t *testing.T) {
	s := newStatusFixture(t)
	s.n.dropMethod = "createForumTopic"
	first := s.post(t, "review-status", "story", "text", "Original", time.Now().Add(time.Hour))
	if first.State != delivery.Uncertain {
		t.Fatal(first)
	}
	snapshot, err := s.n.f.store.InspectTopic(t.Context(), first.JobID)
	if err != nil || snapshot.Scope != "status" {
		t.Fatal(snapshot, err)
	}
	if _, err := s.n.f.store.ReconcileTopic(t.Context(), topicDecision(snapshot, "adopt-status", "adopt", 99)); err != nil {
		t.Fatal(err)
	}
	s.n.mu.Lock()
	s.n.dropMethod = ""
	s.n.mu.Unlock()
	if r := s.n.f.dispatch(t, "ow:review-status", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	s.n.mu.Lock()
	defer s.n.mu.Unlock()
	if len(s.n.calls) != 2 || s.n.calls[1].Method != "sendMessage" || s.n.calls[1].Thread != 99 {
		t.Fatal(s.n.calls)
	}
}

func TestTopicReviewAdoptsMirrorReturnWithGuardUntilNormalCommit(t *testing.T) {
	f := mirrorFixture(t)
	activeMirror(t, f, "group@g.us")
	f.dropTopicAt = 1
	id := mirrorEvent(t, f, "review-return", "return_to_inbox", "group@g.us", -200, "")
	if r := f.dispatch(t, "review-return", "management"); r.State != delivery.Uncertain {
		t.Fatal(r)
	}
	snapshot, err := f.store.InspectTopic(t.Context(), id)
	if err != nil || snapshot.Scope != "mirror-return" {
		t.Fatal(snapshot, err)
	}
	if _, err := f.store.ReconcileTopic(t.Context(), topicDecision(snapshot, "adopt-return", "adopt", 88)); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, "review-return", "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	chat, thread, _ := mirrorRoute(t, f, "group@g.us")
	if chat != -100 || thread != 88 || f.topics.Load() != 1 {
		t.Fatal(chat, thread, f.topics.Load())
	}
}

func TestTopicReviewAdoptsInitialInboxWithoutRepeatingCreation(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.dropTopicAt = 1
	f.topicID = 88
	m := sdk.ChatHistoryMessage{ID: "review-initial", ChatID: "12025550123@c.us", Type: sdk.MsgText, Body: "Original"}
	raw, _ := json.Marshal(map[string]any{"event": sdk.EventMessageReceived, "sessionId": "fixture-session", "idempotencyKey": m.ID, "data": m})
	if !fixtureIngress(t, f)("openwa", raw) {
		t.Fatal("not admitted")
	}
	first := f.dispatch(t, "ow:"+m.ID, "wa")
	if first.State != delivery.Uncertain {
		t.Fatal(first)
	}
	snapshot, err := f.store.InspectTopic(t.Context(), first.JobID)
	if err != nil || snapshot.Scope != "inbox" {
		t.Fatal(snapshot, err)
	}
	if _, err := f.store.ReconcileTopic(t.Context(), topicDecision(snapshot, "adopt-initial", "adopt", 88)); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, "ow:"+m.ID, "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	route, known, err := f.store.ConversationRoute(t.Context(), "fixture-account", m.ChatID)
	if err != nil || !known || route.ThreadID != 88 || f.topics.Load() != 1 {
		t.Fatal(route, known, f.topics.Load(), err)
	}
}
