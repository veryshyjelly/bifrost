package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/attachments"
	"github.com/veryshyjelly/bifrost/internal/bridge"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

func TestNativeRepairCompleteIncomingKeepsExistingPartialTextPlan(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.route(t, "peer@g.us", -200, 0)
	f.failTG = 2
	m := sdk.ChatHistoryMessage{ID: "text-resume", ChatID: "peer@g.us", Author: "12345000000001@lid", Type: sdk.MsgText, Body: strings.Repeat("😀", 4000), IsGroup: true}
	raw, _ := json.Marshal(map[string]any{"event": "message.received", "sessionId": "fixture-session", "idempotencyKey": m.ID, "data": m})
	if !fixtureIngress(t, f)("openwa", raw) {
		t.Fatal("not admitted")
	}
	first := f.dispatch(t, "ow:"+m.ID, "wa")
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	n := newNativeFixture(t, f)
	options := f.dispatchOptions
	options.Extensions.Incoming = n.native.Incoming
	var err error
	f.dispatcher, err = bridge.New(f.store, f.service, f.whatsapp, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", first.JobID); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, "ow:"+m.ID, "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 0 {
		t.Fatal("native replacement sent", n.calls)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tgCalls) != 4 || f.tgCalls[2].Text != f.tgCalls[1].Text {
		t.Fatal(f.tgCalls)
	}
}

func TestNativeRepairPollRetargetsUnexposedControls(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	n.failMethod, n.failCode, n.failDescription, n.failThread = "sendMessage", 400, "Bad Request: message thread not found", 44
	m := sdk.ChatHistoryMessage{ID: "repair-poll", ChatID: "peer@g.us", Author: "12345000000001@lid", Type: sdk.MsgPoll, IsGroup: true, Poll: &sdk.ChatHistoryPoll{Name: "Question", Options: []string{" a ", "b"}}}
	raw, _ := json.Marshal(map[string]any{"event": "message.received", "sessionId": "fixture-session", "idempotencyKey": m.ID, "data": m})
	if !fixtureIngress(t, n.f)("openwa", raw) {
		t.Fatal("not admitted")
	}
	if r := n.f.dispatch(t, "ow:"+m.ID, "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	p, _, err := n.f.store.NativePlan(t.Context(), "fixture-account", relay.NativeIncomingKey("peer@g.us", m.ID))
	if err != nil || len(p.PollChoices) != 2 || p.ThreadID != 99 {
		t.Fatal(p, err)
	}
	var wrong int
	if err := n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM native_poll_actions WHERE (payload->'Target'->>'ThreadID')::bigint<>99 OR payload->>'Choice' NOT IN (' a ','b')").Scan(&wrong); err != nil || wrong != 0 {
		t.Fatal(wrong, err)
	}
	if p.Packets[0].Markup == nil || p.Packets[0].Markup.InlineKeyboard[0][0].CallbackData != "vote:"+p.PollChoices[0].ID {
		t.Fatal(p.Packets)
	}
}

type nativeRepairCrash struct {
	*Store
	fail bool
}

func (s *nativeRepairCrash) CommitNativeRepair(ctx context.Context, j delivery.Job, p relay.NativePlan) (relay.NativePlan, error) {
	if s.fail {
		s.fail = false
		return relay.NativePlan{}, delivery.ErrLocalRetry
	}
	return s.Store.CommitNativeRepair(ctx, j, p)
}

func TestNativeRepairConfirmedTopicSurvivesCrashBeforeRouteCommit(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	n.failMethod, n.failCode, n.failDescription, n.failThread = "sendPhoto", 400, "Bad Request: message thread not found", 44
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
	store := &nativeRepairCrash{Store: n.f.store, fail: true}
	service, err := relay.NewNative(store, n.f.routing, func(_ context.Context, _ string, id string) (relay.NativeTelegram, error) {
		return n.tgClients[id], nil
	}, n.assets, sources, codec, relay.NativeOptions{Session: "fixture-session", OwnerID: 123, Management: -100, MaxBytes: 8 << 20, Identity: n.f.whatsapp, Mentions: n.f.mentionService})
	if err != nil {
		t.Fatal(err)
	}
	options := n.f.dispatchOptions
	options.Extensions.Incoming = service.Incoming
	n.f.dispatcher, err = bridge.New(n.f.store, n.f.service, n.f.whatsapp, options)
	if err != nil {
		t.Fatal(err)
	}
	first := n.incoming(t, "repair-crash", "Caption")
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	var guards int
	if err := n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM route_transition_locks").Scan(&guards); err != nil || guards != 1 {
		t.Fatal(guards, err)
	}
	if _, err := n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", first.JobID); err != nil {
		t.Fatal(err)
	}
	if r := n.f.dispatch(t, "ow:repair-crash", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.reads != 1 || len(n.calls) != 3 || n.calls[0].Method != "sendPhoto" || n.calls[1].Method != "createForumTopic" || n.calls[2].Thread != 99 {
		t.Fatal(n.reads, n.calls)
	}
}

func TestNativeRepairRestoresInboxBeforeSendingWhenPrivateBotUnavailable(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 0)
	if _, err := n.f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET mode='dedicated',inbox_retired=true"); err != nil {
		t.Fatal(err)
	}
	if r := n.incoming(t, "repair-unavailable", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 2 || n.calls[0].Method != "createForumTopic" || n.calls[1].Thread != 99 {
		t.Fatal(n.calls)
	}
}

func TestNativeRepairMissingTopicKeepsFrozenBytesAndChangesGeneration(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	n.failMethod, n.failCode = "sendPhoto", 429
	first := n.incoming(t, "repair-frozen", "Original caption")
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	n.mu.Lock()
	original := append([]byte(nil), n.data...)
	n.data = []byte("source changed")
	n.failCode, n.failDescription, n.failThread = 400, "Bad Request: message thread not found", 44
	n.mu.Unlock()
	if _, err := n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", first.JobID); err != nil {
		t.Fatal(err)
	}
	if r := n.f.dispatch(t, "ow:repair-frozen", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.reads != 1 || len(n.calls) != 4 || n.calls[2].Method != "createForumTopic" || n.calls[3].Thread != 99 || !bytes.Equal(n.calls[3].Body, original) || !strings.Contains(n.calls[3].Text, "Original caption") {
		t.Fatal(n.reads, n.calls)
	}
	p, found, err := n.f.store.NativePlan(t.Context(), "fixture-account", relay.NativeIncomingKey("peer@g.us", "repair-frozen"))
	if err != nil || !found || p.Repairs != 1 || p.Repair != nil || !p.Complete || p.ThreadID != 99 || p.Route.ThreadID != 99 || p.ReturnKey == "" {
		t.Fatal(p, found, err)
	}
	var guards, metadata int
	if err := n.f.store.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM route_transition_locks),(SELECT count(*) FROM jobs WHERE kind='management')").Scan(&guards, &metadata); err != nil || guards != 0 || metadata < 1 {
		t.Fatal(guards, metadata, err)
	}
}

func TestNativeRepairForbiddenMemberFallsBackWithoutMovingMirror(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -200, 0)
	n.f.bind(t, "200", "12345000000001@lid")
	n.failMethod, n.failCode, n.failBot = "sendPhoto", 403, "200"
	if r := n.incoming(t, "repair-member", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 2 || n.calls[0].Bot != "200" || n.calls[1].Bot != "master" || n.calls[1].Chat != -200 || !strings.Contains(n.calls[1].Text, "Caption") {
		t.Fatal(n.calls)
	}
	r, found, err := n.f.store.ConversationRoute(t.Context(), "fixture-account", "peer@g.us")
	if err != nil || !found || r.TelegramChatID != -200 || r.ThreadID != 0 {
		t.Fatal(r, found, err)
	}
	var state string
	if err := n.f.store.pool.QueryRow(t.Context(), "SELECT state FROM effects WHERE effect_key LIKE '%:native:packet:0'").Scan(&state); err != nil || state != "discarded" {
		t.Fatal(state, err)
	}
}

func TestNativeRepairPrivateBotRefusalRestoresRetiredInbox(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 0)
	n.f.bind(t, "200", "12345000000001@lid")
	if _, err := n.f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET mode='dedicated',inbox_retired=true WHERE whatsapp_chat='peer@g.us'"); err != nil {
		t.Fatal(err)
	}
	n.failMethod, n.failCode, n.failBot = "sendPhoto", 403, "200"
	if r := n.incoming(t, "repair-private", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 3 || n.calls[0].Chat != 123 || n.calls[1].Method != "createForumTopic" || n.calls[2].Chat != -100 || n.calls[2].Thread != 99 {
		t.Fatal(n.calls)
	}
	r, _, err := n.f.store.ConversationRoute(t.Context(), "fixture-account", "peer@g.us")
	if err != nil || r.InboxRetired || r.ThreadID != 99 || r.Mode != "dedicated" {
		t.Fatal(r, err)
	}
}

func TestNativeRepairNeverMovesPartialDelivery(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	n.failMethod, n.failCode, n.failDescription, n.failThread = "sendPhoto", 400, "Bad Request: message thread not found", 44
	if r := n.incoming(t, "repair-partial", strings.Repeat("caption ", 200)); r.State != delivery.Failed {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 2 || n.calls[0].Method != "sendMessage" || n.calls[1].Method != "sendPhoto" {
		t.Fatal(n.calls)
	}
	p, _, err := n.f.store.NativePlan(t.Context(), "fixture-account", relay.NativeIncomingKey("peer@g.us", "repair-partial"))
	if err != nil || p.Repairs != 0 || p.Repair != nil {
		t.Fatal(p, err)
	}
}

func TestNativeRepairUnknownTopicCreationRetainsGuard(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	n.failMethod, n.failCode, n.failDescription, n.failThread = "sendPhoto", 400, "Bad Request: message thread not found", 44
	n.dropMethod = "createForumTopic"
	if r := n.incoming(t, "repair-unknown", "Caption"); r.State != delivery.Uncertain {
		t.Fatal(r)
	}
	var guards, unknown int
	if err := n.f.store.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM route_transition_locks),(SELECT count(*) FROM effects WHERE state='uncertain')").Scan(&guards, &unknown); err != nil || guards != 1 || unknown != 1 {
		t.Fatal(guards, unknown, err)
	}
	p, _, err := n.f.store.NativePlan(t.Context(), "fixture-account", relay.NativeIncomingKey("peer@g.us", "repair-unknown"))
	if err != nil || p.Repair == nil || p.ThreadID != 44 {
		t.Fatal(p, err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 2 {
		t.Fatal(n.calls)
	}
}

func TestNativeRepairRejectsStaleRouteSnapshot(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	n.failMethod, n.failCode = "sendPhoto", 429
	first := n.incoming(t, "repair-stale", "Caption")
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	if _, err := n.f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET thread_id=55; UPDATE jobs SET due=clock_timestamp()"); err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	n.failCode, n.failDescription = 400, "Bad Request: message thread not found"
	n.mu.Unlock()
	if r := n.f.dispatch(t, "ow:repair-stale", "wa"); r.State != delivery.Failed {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 2 {
		t.Fatal(n.calls)
	}
}
