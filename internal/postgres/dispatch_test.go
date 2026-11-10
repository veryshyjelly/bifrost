package postgres

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/httpserver"
	"github.com/veryshyjelly/bifrost/internal/ingress"
)

func fixtureIngress(t *testing.T, f *relayFixture) func(string, []byte) bool {
	t.Helper()
	opts := ingress.Options{AccountID: "fixture-account", SessionID: "fixture-session", AdminToken: strings.Repeat("a", 32), WebhookSecret: strings.Repeat("w", 32), OwnerID: 123, ManagementChatID: -100, MasterBotID: 999}
	input, err := ingress.New(opts, f.store)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpserver.Handler(f.store, opts.AdminToken, httpserver.Options{Ingress: input})
	return func(kind string, body []byte) bool {
		t.Helper()
		r := httptest.NewRequest("POST", "/webhooks/"+kind, strings.NewReader(string(body)))
		if kind == "openwa" {
			r.Header.Set("X-OpenWA-Signature", "sha256="+ingress.TelegramSecret(string(body), opts.WebhookSecret))
		} else {
			r.Header.Set("X-Telegram-Bot-Api-Secret-Token", ingress.TelegramSecret(strings.TrimPrefix(kind, "telegram/"), opts.AdminToken))
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("fixture admission status=%d", w.Code)
		}
		var result struct{ Accepted bool }
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Accepted
	}
}
func (f *relayFixture) dispatch(t *testing.T, key, kind string) delivery.Report {
	t.Helper()
	var id int64
	if err := f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return f.run(t, id, kind, f.dispatcher.Handlers()[kind])
}
func TestAuthenticatedNativeEventsRelayThroughDispatcherAndSuppressEcho(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	prepareMentions(t, f)
	admit := fixtureIngress(t, f)
	incoming := sourceEvent(t, "native-in", mentionGroup, map[string]any{"author": mentionBob, "isGroup": true, "body": "😀 Hi @12025550123", "mentionedIds": []string{mentionAlice}, "contact": map[string]string{"name": "Bob"}})
	if !admit("openwa", incoming.Payload) {
		t.Fatal("source not admitted")
	}
	if r := f.dispatch(t, incoming.Dedup, "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	first := f.tgCalls[0]
	f.mu.Unlock()
	if first.Text != "Bob: 😀 Hi @Alice" || first.Chat != -200 || !strings.Contains(first.Entities, `"text_mention"`) {
		t.Fatal(first)
	}
	if admit("openwa", incoming.Payload) {
		t.Fatal("duplicate source was admitted")
	}
	reply := ownerText(71, -200, "Reply @alice_bot")
	reply.ReplyToMessage = &models.Message{ID: 1001}
	update, _ := json.Marshal(models.Update{ID: 101, Message: reply})
	if !admit("telegram/master", update) {
		t.Fatal("owner reply not admitted")
	}
	if r := f.dispatch(t, "tg:master:101", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	sent := f.waCalls[0]
	f.mu.Unlock()
	if sent.ChatID != mentionGroup || sent.QuotedMessageID != "native-in" || sent.Text != "Reply @12025550123" || !reflect.DeepEqual(sent.Mentions, []string{mentionAlice}) {
		t.Fatal(sent)
	}
	var readKey string
	if err := f.store.pool.QueryRow(t.Context(), "SELECT dedup FROM jobs WHERE kind='management' AND payload->>'type'='mark_read'").Scan(&readKey); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, readKey, "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	echo := sourceEvent(t, "native-echo", mentionGroup, map[string]any{"id": "out-1", "author": "12025550999@c.us", "isGroup": true, "fromMe": true, "body": "Reply @12025550123", "mentionedIds": []string{mentionAlice}})
	if !admit("openwa", echo.Payload) {
		t.Fatal("echo not admitted")
	}
	if r := f.dispatch(t, echo.Dedup, "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tgCalls) != 1 || len(f.waCalls) != 1 || f.reads.Load() != 1 || f.sessionCalls.Load() != 0 {
		t.Fatal("echo loop, duplicate send or redundant self lookup")
	}
}
func TestDispatcherCreatesGroupInboxUsingNativeGroupName(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.groupRows = []sdk.GroupParticipant{{ID: mentionBob, Name: "Bob"}}
	admit := fixtureIngress(t, f)
	event := sourceEvent(t, "new-group", mentionGroup, map[string]any{"author": mentionBob, "isGroup": true, "contact": map[string]string{"name": "Bob"}})
	if !admit("openwa", event.Payload) {
		t.Fatal("not admitted")
	}
	if r := f.dispatch(t, event.Dedup, "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	route, found, err := f.store.ConversationRoute(t.Context(), "fixture-account", mentionGroup)
	if err != nil || !found || route.Name != "Group" || route.ThreadID != 44 {
		t.Fatal(route, found, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.topicNames) != 1 || f.topicNames[0] != "Group" || len(f.tgCalls) != 1 || f.groupCalls.Load() != 1 {
		t.Fatal(f.topicNames, f.tgCalls)
	}
}
func TestDispatcherResolvesSelfIndependentlyAndNeverUsesPeerPhone(t *testing.T) {
	f := newRelayFixture(t, nil)
	peer := "987654321@lid"
	f.route(t, peer, -200, 0)
	admit := fixtureIngress(t, f)
	for _, key := range []string{"own-first", "own-second"} {
		event := sourceEvent(t, key, peer, map[string]any{"fromMe": true, "senderPhone": "12025550123", "contact": map[string]string{"name": "Peer", "number": "987654321"}})
		if !admit("openwa", event.Payload) {
			t.Fatal("not admitted")
		}
		if r := f.dispatch(t, event.Dedup, "wa"); r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	id, found, err := f.store.SelfIdentity(t.Context(), "fixture-account")
	if err != nil || !found || id != "12025550999@c.us" {
		t.Fatal(id, found, err)
	}
	d, err := f.store.IdentityDirectory(t.Context(), "fixture-account")
	if err != nil || d.Canonical(peer) != peer {
		t.Fatal("own senderPhone aliased the peer", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessionCalls.Load() != 1 || len(f.tgCalls) != 2 {
		t.Fatal(f.sessionCalls.Load(), len(f.tgCalls))
	}
	for _, call := range f.tgCalls {
		if !strings.HasPrefix(call.Text, "You · sent from WhatsApp: ") || !strings.Contains(call.Entities, "https://wa.me/12025550999") || strings.Contains(call.Entities, "https://wa.me/12025550123") {
			t.Fatal(call)
		}
	}
}
func TestPreparedOwnRelayRetriesWithoutSessionLookup(t *testing.T) {
	f := newRelayFixture(t, nil)
	f.selfPhone = ""
	f.failTG = 1
	peer := "12025550123@c.us"
	f.route(t, peer, -200, 0)
	admit := fixtureIngress(t, f)
	event := sourceEvent(t, "own-rate", peer, map[string]any{"fromMe": true})
	if !admit("openwa", event.Payload) {
		t.Fatal("not admitted")
	}
	r := f.dispatch(t, event.Dedup, "wa")
	if r.State != delivery.Pending || r.Delay != time.Minute {
		t.Fatal(r)
	}
	f.sessionFailure.Store(true)
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	r = f.dispatch(t, event.Dedup, "wa")
	if r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessionCalls.Load() != 1 || len(f.tgCalls) != 2 || f.tgCalls[0] != f.tgCalls[1] || !strings.Contains(f.tgCalls[1].Entities, `https://wa.me/"`) {
		t.Fatal("prepared retry re-resolved self", f.tgCalls, f.sessionCalls.Load())
	}
}

func TestMutedOwnSourceNeedsNoSelfOrGroupProviderLookup(t *testing.T) {
	f := newRelayFixture(t, nil)
	peer := "12025550123@c.us"
	f.route(t, peer, -200, 0)
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET mode='muted' WHERE whatsapp_chat=$1", peer); err != nil {
		t.Fatal(err)
	}
	f.sessionFailure.Store(true)
	admit := fixtureIngress(t, f)
	event := sourceEvent(t, "muted-own", peer, map[string]any{"fromMe": true})
	if !admit("openwa", event.Payload) {
		t.Fatal("not admitted")
	}
	if r := f.dispatch(t, event.Dedup, "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessionCalls.Load() != 0 || f.groupCalls.Load() != 0 || len(f.tgCalls) != 0 {
		t.Fatal("suppressed source reached provider")
	}
}

func TestAliasedContactBotKeepsExistingConversationForReplyAndRetirement(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	lid := "987654321@lid"
	phone := "12025550123@c.us"
	f.route(t, lid, -100, 44)
	admit := fixtureIngress(t, f)
	first := sourceEvent(t, "alias-first", lid, map[string]any{"senderPhone": "12025550123", "contact": map[string]string{"name": "Alice"}})
	if !admit("openwa", first.Payload) {
		t.Fatal("not admitted")
	}
	if r := f.dispatch(t, first.Dedup, "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.bind(t, "200", phone)
	route, found, err := f.store.ConversationRoute(t.Context(), "fixture-account", lid)
	if err != nil || !found || route.Mode != "dedicated" {
		t.Fatal(route, found, err)
	}
	var retiredChat, retiredStream string
	if err := f.store.pool.QueryRow(t.Context(), "SELECT payload->>'chatId',stream FROM jobs WHERE kind='control-retire-inbox'").Scan(&retiredChat, &retiredStream); err != nil || retiredChat != lid || retiredStream != "retire-inbox:"+lid {
		t.Fatal(retiredChat, retiredStream, err)
	}
	// Cleanup execution is a separate pending capability. Represent its completed
	// route state here so subsequent replies exercise the private bot path.
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET inbox_retired=true,thread_id=0 WHERE whatsapp_chat=$1", lid); err != nil {
		t.Fatal(err)
	}
	second := sourceEvent(t, "alias-second", phone, map[string]any{"body": "From peer"})
	if !admit("openwa", second.Payload) {
		t.Fatal("not admitted")
	}
	if r := f.dispatch(t, second.Dedup, "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	copy := f.tgCalls[1]
	f.mu.Unlock()
	if copy.Bot != "child-200" || copy.Chat != 123 || copy.Text != "From peer" {
		t.Fatal(copy)
	}
	reply := ownerText(91, 123, "Reply through child")
	reply.ReplyToMessage = &models.Message{ID: 1002}
	update, _ := json.Marshal(models.Update{ID: 201, Message: reply})
	if !admit("telegram/200", update) {
		t.Fatal("private owner reply not admitted")
	}
	if r := f.dispatch(t, "tg:200:201", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.waCalls) != 1 || f.waCalls[0].ChatID != lid || f.waCalls[0].QuotedMessageID != "alias-second" {
		t.Fatal(f.waCalls)
	}
	var count int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM bridge_routes").Scan(&count); err != nil || count != 1 {
		t.Fatal("alias created a second route", count, err)
	}
}
