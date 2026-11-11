package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

func retirementJob(t *testing.T, f *relayFixture, chat, key string) int64 {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"chatId": chat})
	if _, err := f.store.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: key, Stream: "retire-inbox:" + chat, Kind: "control-retire-inbox", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func activeMirror(t *testing.T, f *relayFixture, chat string) {
	t.Helper()
	f.route(t, chat, -200, 0)
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET mode='group' WHERE whatsapp_chat=$1", chat); err != nil {
		t.Fatal(err)
	}
}

func rememberRetirementTopic(t *testing.T, f *relayFixture, chat string, thread int64) {
	t.Helper()
	if _, err := f.store.pool.Exec(t.Context(), "INSERT INTO conversation_inboxes(account_id,whatsapp_chat,telegram_chat_id,thread_id) VALUES('fixture-account',$1,-100,$2)", chat, thread); err != nil {
		t.Fatal(err)
	}
}

func retryRetirement(t *testing.T, f *relayFixture, id int64) delivery.Report {
	t.Helper()
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	return f.run(t, id, "control-retire-inbox", f.retirer.Retire)
}

func TestStartedBotRetiresInboxWithoutLosingMappingsOrPrivateDelivery(t *testing.T) {
	f := newRelayFixture(t, nil)
	f.route(t, "peer@c.us", -100, 44)
	if r := f.incoming(t, "old-message", relay.Incoming{ChatID: "peer@c.us", MessageID: "wa-old", Body: "hello"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	cleanupRow(t, f.store, "*", "master", -100, 44, 2000, "protected", time.Now().UnixMilli(), 0)
	cleanupRow(t, f.store, "*", "master", -100, 1, 2001, "chat", time.Now().UnixMilli(), 0)
	cleanupRow(t, f.store, "bot:200", "200", 123, 44, 2002, "chat", time.Now().UnixMilli(), 0)
	f.bind(t, "200", "peer@c.us")
	var key string
	if err := f.store.pool.QueryRow(t.Context(), "SELECT dedup FROM jobs WHERE kind='control-retire-inbox'").Scan(&key); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, key, "control-retire-inbox"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if !reflect.DeepEqual(f.retireCalls, []retireCall{{Bot: "master-fixture", Chat: -100, Thread: 44}}) {
		t.Fatal(f.retireCalls)
	}
	route, _, err := f.store.ConversationRoute(t.Context(), "fixture-account", "peer@c.us")
	if err != nil || !route.InboxRetired || route.ThreadID != 0 || route.Mode != conversation.Dedicated {
		t.Fatal(route, err)
	}
	if !rowDeleted(t, f.store, "*", -100, 1001) || !rowDeleted(t, f.store, "*", -100, 2000) || rowDeleted(t, f.store, "*", -100, 2001) || rowDeleted(t, f.store, "bot:200", 123, 2002) {
		t.Fatal("topic deletion escaped its scope")
	}
	_, found, err := f.store.WhatsAppQuote(t.Context(), "fixture-account", "peer@c.us", "wa-old", "master", -100, 44)
	if err != nil || found {
		t.Fatal("deleted topic was quoted", found, err)
	}
	var count int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_mappings").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if r := f.incoming(t, "private-after-retirement", relay.Incoming{ChatID: "peer@c.us", SenderID: "peer@c.us", MessageID: "wa-new", Body: "still delivered"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if last := f.tgCalls[len(f.tgCalls)-1]; last.Bot != "child-200" || last.Chat != 123 || last.Thread != 0 {
		t.Fatal(last)
	}
}

func TestRetirementRefusesSharedRoutesMappingsAndPreparedPlans(t *testing.T) {
	for _, kind := range []string{"route", "mapping without observation", "prepared plan"} {
		t.Run(kind, func(t *testing.T) {
			f := newRelayFixture(t, nil)
			activeMirror(t, f, "peer@g.us")
			rememberRetirementTopic(t, f, "peer@g.us", 44)
			rememberRetirementTopic(t, f, "peer@g.us", 43)
			switch kind {
			case "route":
				f.route(t, "other@g.us", -100, 44)
			case "mapping without observation":
				if _, err := f.store.pool.Exec(t.Context(), "INSERT INTO message_mappings(account_id,scope,telegram_chat_id,telegram_message_id,thread_id,bot_id,whatsapp_chat,whatsapp_message_id,direction,receipt) VALUES('fixture-account','*',-100,7,44,'master','other@g.us','other','in','{}')"); err != nil {
					t.Fatal(err)
				}
			case "prepared plan":
				id := f.enqueue(t, "wa", "foreign-plan", "wa:other@g.us", "")
				p, _ := json.Marshal(relay.Plan{TelegramChat: -100, ThreadID: 44})
				if _, err := f.store.pool.Exec(t.Context(), "INSERT INTO relay_plans(account_id,plan_key,job_id,direction,whatsapp_chat,payload,complete) VALUES('fixture-account','foreign',$1,'in','other@g.us',$2,true)", id, p); err != nil {
					t.Fatal(err)
				}
				if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET state='done' WHERE dedup='foreign-plan'"); err != nil {
					t.Fatal(err)
				}
			}
			id := retirementJob(t, f, "peer@g.us", "retire-shared")
			if r := f.run(t, id, "control-retire-inbox", f.retirer.Retire); r.State != delivery.Failed {
				t.Fatal(r)
			}
			var count int
			if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM retired_topics").Scan(&count); err != nil || count != 0 || len(f.retireCalls) != 0 {
				t.Fatal(count, f.retireCalls, err)
			}
		})
	}
}

func TestRetirementRetriesLostResponseAgainstFrozenTopicAfterRouteChanges(t *testing.T) {
	f := newRelayFixture(t, nil)
	activeMirror(t, f, "peer@g.us")
	rememberRetirementTopic(t, f, "peer@g.us", 44)
	f.retireDropAt = 1
	id := retirementJob(t, f, "peer@g.us", "retire-lost")
	if r := f.run(t, id, "control-retire-inbox", f.retirer.Retire); r.State != delivery.Pending {
		t.Fatal(r)
	}
	// Recreate the consumer over a new pool, as after process restart.
	reopened, err := Open(t.Context(), f.store.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	f.store.Close()
	f.store = reopened
	f.retirer, err = conversation.NewRetirer(reopened, f.topicDeleter, -100)
	if err != nil {
		t.Fatal(err)
	}
	// A later restoration can use a fresh topic. It cannot rebind the reserved
	// ID, and retry must not widen its snapshot to include this fresh inbox.
	f.route(t, "peer@g.us", -100, 99)
	if err := f.store.PutIngressRoute(t.Context(), "fixture-account", ingress.Route{WhatsAppChat: "other@g.us", TelegramChatID: -100, ThreadID: 44}); !errors.Is(err, conversation.ErrInboxRequired) {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.retireMissing = true
	f.mu.Unlock()
	if r := retryRetirement(t, f, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(f.retireCalls) != 2 || f.retireCalls[0] != f.retireCalls[1] || f.retireCalls[1].Thread != 44 {
		t.Fatal(f.retireCalls)
	}
	route, _, err := f.store.ConversationRoute(t.Context(), "fixture-account", "peer@g.us")
	if err != nil || route.ThreadID != 99 || route.InboxRetired {
		t.Fatal(route, err)
	}
	var unsafe int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM effects WHERE state IN ('sending','uncertain')").Scan(&unsafe); err != nil || unsafe != 0 {
		t.Fatal(unsafe, err)
	}
}

func TestRetirementSerializesWithUncommittedRouteAdmission(t *testing.T) {
	f := newRelayFixture(t, nil)
	activeMirror(t, f, "peer@g.us")
	rememberRetirementTopic(t, f, "peer@g.us", 44)
	retirementJob(t, f, "peer@g.us", "retire-concurrent")
	job := claim(t, f.store)
	tx, err := f.store.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	q := f.store.queries.WithTx(tx)
	if err := q.LockContactDirectory(t.Context(), "fixture-account"); err != nil {
		t.Fatal(err)
	}
	if err := q.PutIngressRoute(t.Context(), db.PutIngressRouteParams{AccountID: "fixture-account", WhatsappChat: "other@g.us", TelegramChatID: -100, ThreadID: 44}); err != nil {
		t.Fatal(err)
	}
	ready := make(chan error, 1)
	go func() { _, err := f.store.PrepareInboxRetirement(t.Context(), job, -100); ready <- err }()
	select {
	case err := <-ready:
		t.Fatal("retirement bypassed the route-admission lock", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-ready:
		if !errors.Is(err, conversation.ErrSharedInbox) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("retirement did not observe committed route")
	}
	var count int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM retired_topics").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestRetirementFencesStaleAndCrossAccountWritesAndProviderCalls(t *testing.T) {
	f := newRelayFixture(t, nil)
	activeMirror(t, f, "peer@g.us")
	rememberRetirementTopic(t, f, "peer@g.us", 44)
	retirementJob(t, f, "peer@g.us", "retire-fenced")
	job := claim(t, f.store)
	other := job
	other.AccountID = "other-account"
	if _, err := f.store.PrepareInboxRetirement(t.Context(), other, -100); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
	// The database claim alone cannot authorize an API call without the local
	// worker's current lease. Preparing a journal is still safe and reversible.
	if err := f.retirer.Retire(t.Context(), job, &delivery.Effects{}); !errors.Is(err, delivery.ErrInvalid) || len(f.retireCalls) != 0 {
		t.Fatal(err, f.retireCalls)
	}
	expire(t, f.store, job)
	if err := f.store.FinishTopicRetirement(t.Context(), job, 44); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
	if err := f.store.CompleteInboxRetirement(t.Context(), job); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
}

func TestRetirementPermissionFailureDoesNotBlockPrivateDelivery(t *testing.T) {
	f := newRelayFixture(t, nil)
	f.route(t, "peer@c.us", -100, 44)
	f.bind(t, "200", "peer@c.us")
	f.retireStatus = 403
	var key string
	if err := f.store.pool.QueryRow(t.Context(), "SELECT dedup FROM jobs WHERE kind='control-retire-inbox'").Scan(&key); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, key, "control-retire-inbox"); r.State != delivery.Failed {
		t.Fatal(r)
	}
	route, _, err := f.store.ConversationRoute(t.Context(), "fixture-account", "peer@c.us")
	if err != nil || route.ThreadID != 44 || route.InboxRetired {
		t.Fatal(route, err)
	}
	if r := f.incoming(t, "after-refusal", relay.Incoming{ChatID: "peer@c.us", SenderID: "peer@c.us", MessageID: "after-refusal", Body: "private works"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(f.retireCalls) != 1 || f.tgCalls[0].Bot != "child-200" {
		t.Fatal(f.retireCalls, f.tgCalls)
	}
	var id int64
	if err := f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Review(t.Context(), id, true, false); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.retireStatus = 0
	f.mu.Unlock()
	if r := retryRetirement(t, f, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(f.retireCalls) != 2 || f.retireCalls[0] != f.retireCalls[1] {
		t.Fatal(f.retireCalls)
	}
}

func TestRetirementRebindingDuringProviderCallProtectsTheNewInbox(t *testing.T) {
	f := newRelayFixture(t, nil)
	f.route(t, "peer@c.us", -100, 44)
	f.bind(t, "200", "peer@c.us")
	results := make(chan [2]error, 1)
	f.retireBefore = func() {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		old := f.store.PutIngressRoute(ctx, "fixture-account", ingress.Route{WhatsAppChat: "other@g.us", TelegramChatID: -100, ThreadID: 44})
		fresh := f.store.PutIngressRoute(ctx, "fixture-account", ingress.Route{WhatsAppChat: "peer@c.us", TelegramChatID: -100, ThreadID: 99})
		results <- [2]error{old, fresh}
	}
	var key string
	if err := f.store.pool.QueryRow(t.Context(), "SELECT dedup FROM jobs WHERE kind='control-retire-inbox'").Scan(&key); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, key, "control-retire-inbox"); r.State != delivery.Done {
		t.Fatal(r)
	}
	result := <-results
	if !errors.Is(result[0], conversation.ErrInboxRequired) || result[1] != nil {
		t.Fatal(result)
	}
	route, _, err := f.store.ConversationRoute(t.Context(), "fixture-account", "peer@c.us")
	if err != nil || route.ThreadID != 99 || route.InboxRetired {
		t.Fatal(route, err)
	}
}

func TestRetirementDiscoversEmptyAndMappedFormerInboxesAndPreservesGeneral(t *testing.T) {
	f := newRelayFixture(t, nil)
	f.route(t, "peer@g.us", -100, 45)
	if r := f.incoming(t, "mapped-former", relay.Incoming{ChatID: "peer@g.us", MessageID: "mapped", Body: "old topic"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	activeMirror(t, f, "peer@g.us")
	rememberRetirementTopic(t, f, "peer@g.us", 44)
	cleanupRow(t, f.store, "*", "master", -100, 1, 2000, "chat", 0, 0)
	id := retirementJob(t, f, "peer@g.us", "retire-two")
	if r := f.run(t, id, "control-retire-inbox", f.retirer.Retire); r.State != delivery.Done {
		t.Fatal(r)
	}
	if !reflect.DeepEqual(f.retireCalls, []retireCall{{Bot: "master-fixture", Chat: -100, Thread: 44}, {Bot: "master-fixture", Chat: -100, Thread: 45}}) || rowDeleted(t, f.store, "*", -100, 2000) {
		t.Fatal(f.retireCalls)
	}
	route, _, err := f.store.ConversationRoute(t.Context(), "fixture-account", "peer@g.us")
	if err != nil || route.TelegramChatID != -200 || route.ThreadID != 0 || route.InboxRetired {
		t.Fatal(route, err)
	}
}

func TestRetirementWaitsForFrozenRelayAndBlocksNewPlansAfterReservation(t *testing.T) {
	f := newRelayFixture(t, nil)
	f.route(t, "peer@c.us", -100, 44)
	f.failTG = 1
	m := relay.Incoming{ChatID: "peer@c.us", MessageID: "pending", Body: "send pending"}
	if r := f.incoming(t, "pending-relay", m); r.State != delivery.Pending {
		t.Fatal(r)
	}
	f.bind(t, "200", "peer@c.us")
	var id int64
	if err := f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='control-retire-inbox'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if r := f.run(t, id, "control-retire-inbox", f.retirer.Retire); r.State != delivery.Pending || len(f.retireCalls) != 0 {
		t.Fatal(r, f.retireCalls)
	}
	// The original prepared relay finishes at its frozen destination before
	// deletion; completed sends are never redirected to the new private route.
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE dedup='pending-relay'"); err != nil {
		t.Fatal(err)
	}
	if r := f.incoming(t, "pending-relay", m); r.State != delivery.Done || f.tgCalls[len(f.tgCalls)-1].Chat != -100 {
		t.Fatal(r, f.tgCalls)
	}
	f.mu.Lock()
	f.retireStatus = 429
	f.mu.Unlock()
	if r := retryRetirement(t, f, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	// Use a claimed job to test admission with an already computed old target.
	p := relay.Plan{Key: "stale-plan", Prefix: "stale", Direction: "in", WhatsAppChat: "peer@c.us", TelegramChat: -100, ThreadID: 44, BotID: "master", Parts: []relay.TelegramRequest{{ChatID: -100, ThreadID: 44, Text: "stale"}}}
	p.Route, _, _ = f.store.ConversationRoute(t.Context(), "fixture-account", "peer@c.us")
	staleID := f.enqueue(t, "wa", "stale-plan", "wa:peer@c.us", "")
	if r := f.run(t, staleID, "wa", func(ctx context.Context, j delivery.Job, _ *delivery.Effects) error {
		_, err := f.store.PrepareRelay(ctx, j, p)
		if !errors.Is(err, conversation.ErrInboxRequired) {
			t.Errorf("stale target accepted: %v", err)
		}
		return nil
	}); r.State != delivery.Done {
		t.Fatal(r)
	}
}

func TestRetirementLocalCommitFailureRetriesSameTopicAndCommittedOutcomeIsReused(t *testing.T) {
	for _, stage := range []string{"outcome", "completion"} {
		t.Run(stage, func(t *testing.T) {
			f := newRelayFixture(t, nil)
			activeMirror(t, f, "peer@g.us")
			rememberRetirementTopic(t, f, "peer@g.us", 44)
			constraint := "ALTER TABLE retired_topics ADD CONSTRAINT fixture_failure CHECK (NOT deleted)"
			drop := "ALTER TABLE retired_topics DROP CONSTRAINT fixture_failure"
			if stage == "completion" {
				constraint = "ALTER TABLE inbox_retirements ADD CONSTRAINT fixture_failure CHECK (NOT complete)"
				drop = "ALTER TABLE inbox_retirements DROP CONSTRAINT fixture_failure"
			}
			if _, err := f.store.pool.Exec(t.Context(), constraint); err != nil {
				t.Fatal(err)
			}
			id := retirementJob(t, f, "peer@g.us", "retire-commit")
			if r := f.run(t, id, "control-retire-inbox", f.retirer.Retire); r.State != delivery.Pending {
				t.Fatal(r)
			}
			if _, err := f.store.pool.Exec(t.Context(), drop); err != nil {
				t.Fatal(err)
			}
			if r := retryRetirement(t, f, id); r.State != delivery.Done {
				t.Fatal(r)
			}
			want := 1
			if stage == "outcome" {
				want = 2
			}
			if len(f.retireCalls) != want {
				t.Fatal("outcome replay changed", f.retireCalls)
			}
		})
	}
}

func TestRetirementNoopCannotGrowIntoLaterActivationAndGeneralIsNotDeleted(t *testing.T) {
	for _, thread := range []int64{0, 1, 44} {
		t.Run(fmt.Sprint(thread), func(t *testing.T) {
			f := newRelayFixture(t, nil)
			f.route(t, "peer@c.us", -100, thread)
			id := retirementJob(t, f, "peer@c.us", "early-retirement")
			if r := f.run(t, id, "control-retire-inbox", f.retirer.Retire); r.State != delivery.Done || len(f.retireCalls) != 0 {
				t.Fatal(r, f.retireCalls)
			}
			f.bind(t, "200", "peer@c.us")
			var key string
			if err := f.store.pool.QueryRow(t.Context(), "SELECT dedup FROM jobs WHERE kind='control-retire-inbox' AND state='pending'").Scan(&key); err != nil {
				t.Fatal(err)
			}
			if r := f.dispatch(t, key, "control-retire-inbox"); r.State != delivery.Done {
				t.Fatal(r)
			}
			want := 0
			if thread > 1 {
				want = 1
			}
			if len(f.retireCalls) != want {
				t.Fatal(f.retireCalls)
			}
			route, _, err := f.store.ConversationRoute(t.Context(), "fixture-account", "peer@c.us")
			if err != nil || !route.InboxRetired || route.ThreadID != 0 {
				t.Fatal(route, err)
			}
		})
	}
}
