package ingress

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

type fixtureStore struct {
	mu               sync.Mutex
	jobs             map[string]delivery.Event
	observations     []Observation
	bots             map[string]Bot
	routes           []Route
	err              error
	entered, release chan struct{}
}

func (f *fixtureStore) IngressBot(_ context.Context, _ string, id string) (Bot, bool, error) {
	b, ok := f.bots[id]
	return b, ok, nil
}
func (f *fixtureStore) IngressRoute(_ context.Context, _ string, chat, thread, management int64) (Route, bool, error) {
	for _, r := range f.routes {
		if r.TelegramChatID == chat && (r.ThreadID == 0 || r.ThreadID == thread) && !(chat == management && r.InboxRetired && r.ThreadID == 0) {
			return r, true, nil
		}
	}
	return Route{}, false, nil
}
func (f *fixtureStore) Admit(ctx context.Context, e *delivery.Event, o *Observation) (bool, error) {
	if f.entered != nil {
		close(f.entered)
		select {
		case <-f.release:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	if o != nil {
		f.observations = append(f.observations, *o)
	}
	if e == nil {
		return false, nil
	}
	if f.jobs == nil {
		f.jobs = map[string]delivery.Event{}
	}
	if _, ok := f.jobs[e.Dedup]; ok {
		return false, nil
	}
	f.jobs[e.Dedup] = *e
	return true, nil
}
func fixture(t *testing.T, f *fixtureStore) *Service {
	t.Helper()
	s, err := New(Options{AccountID: "fixture-account", SessionID: "current", WebhookSecret: strings.Repeat("w", 32), AdminToken: strings.Repeat("fixture-admin-secret-", 2), OwnerID: 123, ManagementChatID: -100, MasterBotID: 999, Now: func() time.Time { return time.UnixMilli(1794300000000) }}, f)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func request(s *Service, path, body string, auth bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if auth {
		if path == "/webhooks/openwa" {
			r.Header.Set("X-OpenWA-Signature", "sha256="+TelegramSecret(body, s.c.WebhookSecret))
		} else if m := botPath.FindStringSubmatch(path); m != nil {
			r.Header.Set("X-Telegram-Bot-Api-Secret-Token", TelegramSecret(m[1], s.c.AdminToken))
		}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

const wa = `{"sessionId":"current","event":"message.received","idempotencyKey":"event-1","data":{"chatId":"fixture@g.us","id":"m1"},"accountId":"forged"}`

func TestOpenWAVerifiesRawBytesBeforeDurableAcceptance(t *testing.T) {
	f := &fixtureStore{}
	s := fixture(t, f)
	if w := request(s, "/webhooks/openwa", wa, false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	r := httptest.NewRequest("POST", "/webhooks/openwa", strings.NewReader(strings.Replace(wa, "m1", "m2", 1)))
	r.Header.Set("X-OpenWA-Signature", "sha256="+TelegramSecret(wa, s.c.WebhookSecret))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(s, "/webhooks/openwa", wa, true); w.Code != 200 || !strings.Contains(w.Body.String(), `"accepted":true`) {
		t.Fatal(w.Code, w.Body)
	}
	if w := request(s, "/webhooks/openwa", wa, true); !strings.Contains(w.Body.String(), `"accepted":false`) {
		t.Fatal(w.Body)
	}
	e := f.jobs["ow:event-1"]
	if e.AccountID != "fixture-account" || e.Stream != "wa:fixture@g.us" || e.Kind != "wa" || text(object(e.Payload)["accountId"]) != "fixture-account" {
		t.Fatal(e)
	}
	for _, event := range []string{"message.sent", "message.edited", "message.revoked", "message.reaction", "poll.vote", "status.received", "session.status", "session.disconnected", "message.failed"} {
		body := strings.ReplaceAll(strings.Replace(wa, "message.received", event, 1), "event-1", event)
		if w := request(s, "/webhooks/openwa", body, true); w.Code != 200 {
			t.Fatalf("%s: %d", event, w.Code)
		}
	}
	if f.jobs["ow:status.received"].Stream != "wa:status@broadcast" {
		t.Fatal("status stream split")
	}
}

func TestIngressRejectsMalformedAndOversizedBodies(t *testing.T) {
	s := fixture(t, &fixtureStore{})
	for _, body := range []string{`null`, `[]`, `1`, `"text"`, `{}`, `{} {}`, `{`, strings.Replace(wa, "current", "old", 1), strings.Replace(wa, "message.received", "message.ack", 1), strings.Replace(wa, `"event-1"`, `""`, 1)} {
		if w := request(s, "/webhooks/openwa", body, true); w.Code != 400 {
			t.Fatalf("body %.70s: %d", body, w.Code)
		}
	}
	if w := request(s, "/webhooks/openwa", strings.Repeat("x", MaxBody+1), false); w.Code != 413 {
		t.Fatal(w.Code)
	}
	if w := request(s, "/webhooks/telegram/master", strings.Repeat("x", MaxBody+1), false); w.Code != 401 {
		t.Fatal("Telegram must authenticate before body", w.Code)
	}
	if w := request(s, "/webhooks/telegram/master", strings.Repeat("x", MaxBody+1), true); w.Code != 413 {
		t.Fatal(w.Code)
	}
	for _, path := range []string{"/unknown", "/webhooks/telegram/bad/path", "/webhooks/telegram/"} {
		if w := request(s, path, `{}`, true); w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/webhooks/openwa", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestWebhookDoesNotAcknowledgeBeforeCommitAndHidesFailure(t *testing.T) {
	f := &fixtureStore{entered: make(chan struct{}), release: make(chan struct{})}
	s := fixture(t, f)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- request(s, "/webhooks/openwa", wa, true) }()
	<-f.entered
	select {
	case <-done:
		t.Fatal("acknowledged uncommitted work")
	default:
	}
	close(f.release)
	if w := <-done; w.Code != 200 {
		t.Fatal(w.Code)
	}
	f.entered = nil
	f.err = errors.New("postgres://private-token@private-host/db")
	if w := request(s, "/webhooks/openwa", wa, true); w.Code != 503 || strings.Contains(w.Body.String(), "private") {
		t.Fatal(w.Code, w.Body)
	}
}

func TestTelegramOwnerAdmissionBotIsolationAndGroupStreams(t *testing.T) {
	message := func(chat int64, kind, text string, thread int, topic bool) map[string]any {
		return map[string]any{"message_id": 7, "date": 1794300000, "from": map[string]any{"id": 123, "is_bot": false}, "chat": map[string]any{"id": chat, "type": kind}, "text": text, "message_thread_id": thread, "is_topic_message": topic}
	}
	cases := []struct {
		name, bot string
		m         map[string]any
		accepted  bool
		stream    string
		code      int
	}{
		{"master private", "master", message(123, "private", "hello", 0, false), true, "tg:master:123:0", 200},
		{"contact private", "200", message(123, "private", "hello", 0, false), true, "tg:200:123:0", 200},
		{"ordinary group member bot", "200", message(-200, "supergroup", "hello", 999, true), false, "", 200},
		{"directed group member bot", "200", message(-200, "supergroup", "/history@PEERBOT 10", 999, true), true, "tg:-200:0", 200},
		{"wrong recipient", "200", message(-200, "supergroup", "/history@someone 10", 0, false), false, "", 200},
		{"whole group ignores reply thread", "master", message(-200, "supergroup", "hello", 999, true), true, "tg:-200:0", 200},
		{"management topic", "master", message(-100, "supergroup", "hello", 44, true), true, "tg:-100:44", 200},
		{"unknown nonforum reply", "master", message(-300, "supergroup", "hello", 88, false), true, "tg:-300:0", 200},
		{"unknown forum topic", "master", message(-300, "supergroup", "hello", 88, true), true, "tg:-300:88", 200},
		{"unknown bot", "404", message(123, "private", "hello", 0, false), false, "", 404},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fixtureStore{bots: map[string]Bot{"200": {ID: "200", Username: "PeerBot"}}, routes: []Route{{WhatsAppChat: "mirror@g.us", TelegramChatID: -200}, {WhatsAppChat: "inbox@g.us", TelegramChatID: -100, ThreadID: 44}}}
			s := fixture(t, f)
			raw, _ := json.Marshal(map[string]any{"update_id": 10, "message": tc.m})
			w := request(s, "/webhooks/telegram/"+tc.bot, string(raw), true)
			if w.Code != tc.code {
				t.Fatal(w.Code, w.Body)
			}
			e, ok := f.jobs["tg:"+tc.bot+":10"]
			if ok != tc.accepted {
				t.Fatal("admission", ok, w.Body)
			}
			if ok && e.Stream != tc.stream {
				t.Fatal(e.Stream, tc.stream)
			}
		})
	}
}

func TestTelegramRetainsCleanupObservationsWithoutUnauthorizedDelivery(t *testing.T) {
	f := &fixtureStore{}
	s := fixture(t, f)
	for _, sender := range []string{`{"id":456,"is_bot":false}`, `{"id":123,"is_bot":true}`} {
		body := `{"update_id":1,"message":{"message_id":7,"date":1794300000,"chat":{"id":-100,"type":"supergroup"},"from":` + sender + `,"forum_topic_created":{"name":"Topic"}}}`
		w := request(s, "/webhooks/telegram/master", body, true)
		if w.Code != 200 || len(f.jobs) != 0 || !strings.Contains(w.Body.String(), "false") {
			t.Fatal(w.Code, w.Body)
		}
	}
	if len(f.observations) != 2 || f.observations[0].Kind != "protected" || f.observations[0].Scope != "*" {
		t.Fatal(f.observations)
	}
	for _, id := range []string{`null`, `1.5`, `9007199254740992`, `"1"`} {
		body := `{"update_id":` + id + `,"message":{"message_id":7,"from":{"id":123},"chat":{"id":123,"type":"private"}}}`
		if w := request(s, "/webhooks/telegram/master", body, true); w.Code != 400 {
			t.Fatal(id, w.Code, w.Body)
		}
	}
	if w := request(s, "/webhooks/telegram/master", `{"update_id":1e2,"message":{"message_id":7,"from":{"id":123},"chat":{"id":123,"type":"private"}}}`, true); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if _, ok := f.jobs["tg:master:100"]; !ok {
		t.Fatal("safe exponent update ID changed")
	}
}

func TestTelegramNativeOwnerUpdateTypesAndCrossBotSecrets(t *testing.T) {
	for _, tc := range []struct{ field, body, stream string }{
		{"message_reaction", `{"user":{"id":123},"chat":{"id":-200,"type":"supergroup"}}`, "tg-reaction:master:-200"},
		{"my_chat_member", `{"from":{"id":123},"chat":{"id":-200,"type":"supergroup"}}`, "tg:master"},
		{"poll_answer", `{"user":{"id":123},"poll_id":"p","option_ids":[0]}`, "tg:master"},
		{"managed_bot", `{"user":{"id":123},"bot":{"id":200,"is_bot":true}}`, "tg:master"},
		{"callback_query", `{"id":"c","from":{"id":123},"message":{"date":0,"message_id":7,"chat":{"id":-200,"type":"supergroup"}}}`, "tg:-200:0"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			f := &fixtureStore{}
			s := fixture(t, f)
			w := request(s, "/webhooks/telegram/master", `{"update_id":1,"`+tc.field+`":`+tc.body+`}`, true)
			if w.Code != 200 || f.jobs["tg:master:1"].Stream != tc.stream {
				t.Fatal(w.Code, w.Body, f.jobs)
			}
		})
	}
	s := fixture(t, &fixtureStore{})
	r := httptest.NewRequest("POST", "/webhooks/telegram/200", strings.NewReader(`{}`))
	r.Header.Set("X-Telegram-Bot-Api-Secret-Token", TelegramSecret("master", s.c.AdminToken))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func TestManagementUsesBaselineJavaScriptSignatureAndTimestampWindow(t *testing.T) {
	var fixtures []struct{ Raw, Canonical, Signature string }
	data, err := os.ReadFile("testdata/management.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, gold := range fixtures {
		f := &fixtureStore{}
		s := fixture(t, f)
		raw := strings.TrimSuffix(gold.Raw, "}") + `,"signature":"` + gold.Signature + `"}`
		w := request(s, "/v1/management", raw, false)
		if w.Code != 200 || len(f.jobs) != 1 {
			t.Fatal(w.Code, w.Body)
		}
		for _, e := range f.jobs {
			if string(e.Payload) != gold.Canonical {
				t.Fatalf("canonical mismatch %s != %s", e.Payload, gold.Canonical)
			}
		}
		if w := request(s, "/v1/management", raw, false); !strings.Contains(w.Body.String(), `"accepted":false`) {
			t.Fatal(w.Body)
		}
		if w := request(s, "/v1/management", strings.Replace(raw, "new_conversation", "edited", 1), false); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	s := fixture(t, &fixtureStore{})
	s.c.Now = func() time.Time { return time.UnixMilli(1794300000000).Add(7*24*time.Hour + time.Millisecond) }
	raw := strings.TrimSuffix(fixtures[0].Raw, "}") + `,"signature":"` + fixtures[0].Signature + `"}`
	if w := request(s, "/v1/management", raw, false); w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func TestTelegramCaseAliasesCannotReplaceActorIdentity(t *testing.T) {
	for _, from := range []string{`{"id":456,"ID":123,"is_bot":false}`, `{"id":123,"is_bot":true,"IS_BOT":false}`} {
		f := &fixtureStore{}
		s := fixture(t, f)
		body := `{"update_id":1,"message":{"message_id":7,"from":` + from + `,"chat":{"id":123,"type":"private"}}}`
		w := request(s, "/webhooks/telegram/master", body, true)
		if w.Code != 200 || len(f.jobs) != 0 {
			t.Fatal("actor alias admitted", w.Code, w.Body)
		}
	}
}

func TestDirectedCommandParsingMatchesBaselineUnicodeWhitespace(t *testing.T) {
	var fixtures []struct {
		Value, Recipient string
		Match            bool
	}
	data, err := os.ReadFile("testdata/commands.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		parts := commandParts(&models.Message{Text: f.Value})
		if (parts != nil) != f.Match || (parts != nil && parts[2] != f.Recipient) {
			t.Fatalf("baseline command mismatch %q: %v", f.Value, parts)
		}
	}
}
