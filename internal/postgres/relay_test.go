package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/mentions"
	wa "github.com/veryshyjelly/bifrost/internal/providers/openwa"
	tg "github.com/veryshyjelly/bifrost/internal/providers/telegram"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

type tgTextCall struct {
	Bot, Text, Entities, Reply, Preview, ParseMode string
	Chat, Thread                                   int64
}
type waTextCall struct {
	ChatID, Text, QuotedMessageID, Key string
	Mentions                           []string
}
type relayFixture struct {
	store                *Store
	service              *relay.Service
	mu                   sync.Mutex
	tgCalls              []tgTextCall
	waCalls              []waTextCall
	tgCount              int
	failTG               int
	perBotIDs            bool
	botSeq               map[string]int
	reads                atomic.Int32
	topics               atomic.Int32
	readFailure          atomic.Bool
	waEntered, waRelease chan struct{}
	groupRows            []sdk.GroupParticipant
	groupCalls           atomic.Int32
	groupFailure         atomic.Bool
	memberLeft           atomic.Bool
	memberCalls          atomic.Int32
	failWA               int
}

func newRelayFixture(t *testing.T, wrap func(*Store) relay.Store, withMentions ...bool) *relayFixture {
	t.Helper()
	f := &relayFixture{store: testStore(t), botSeq: map[string]int{}}
	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		if strings.HasSuffix(r.URL.Path, "createForumTopic") {
			f.topics.Add(1)
			fmt.Fprint(w, `{"ok":true,"result":{"message_thread_id":44,"name":"Fixture"}}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "getChatMember") {
			f.memberCalls.Add(1)
			status := "member"
			if f.memberLeft.Load() {
				status = "left"
			}
			fmt.Fprintf(w, `{"ok":true,"result":{"status":%q,"user":{"id":200,"is_bot":true,"first_name":"Alice"}}}`, status)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "sendMessage") {
			t.Error("unexpected Telegram operation")
			w.WriteHeader(500)
			return
		}
		chat, _ := strconv.ParseInt(r.FormValue("chat_id"), 10, 64)
		thread, _ := strconv.ParseInt(r.FormValue("message_thread_id"), 10, 64)
		token := strings.Split(strings.TrimPrefix(r.URL.Path, "/bot"), "/")[0]
		f.mu.Lock()
		defer f.mu.Unlock()
		f.tgCount++
		f.tgCalls = append(f.tgCalls, tgTextCall{Bot: token, Text: r.FormValue("text"), Entities: r.FormValue("entities"), Reply: r.FormValue("reply_parameters"), Preview: r.FormValue("link_preview_options"), ParseMode: r.FormValue("parse_mode"), Chat: chat, Thread: thread})
		if f.failTG == f.tgCount {
			fmt.Fprint(w, `{"ok":false,"error_code":429,"parameters":{"retry_after":60},"description":"fixture rate refusal"}`)
			return
		}
		id := 1000 + f.tgCount
		if f.perBotIDs {
			f.botSeq[token]++
			id = 6 + f.botSeq[token]
		}
		// Telegram may include a reply-thread ID in a non-forum whole group.
		if thread == 0 && r.FormValue("reply_parameters") != "" {
			thread = 999
		}
		b, _ := json.Marshal(map[string]any{"ok": true, "result": map[string]any{"message_id": id, "date": 1700000000, "message_thread_id": thread, "is_topic_message": false, "chat": map[string]any{"id": chat, "type": "supergroup"}}})
		w.Write(b)
	}))
	t.Cleanup(tgServer.Close)
	waServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/groups/") && r.Method == "GET" {
			f.groupCalls.Add(1)
			if f.groupFailure.Load() {
				w.WriteHeader(503)
				return
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"id": "123@g.us", "name": "Group", "participants": f.groupRows})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/chats/read") {
			f.reads.Add(1)
			if f.readFailure.Load() {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"message":"fixture unavailable"}`)
			} else {
				fmt.Fprint(w, `{"success":true}`)
			}
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/messages/send-text") {
			t.Error("unexpected WhatsApp operation")
			w.WriteHeader(500)
			return
		}
		var body struct {
			ChatID, Text, QuotedMessageID string
			Mentions                      []string
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.waCalls = append(f.waCalls, waTextCall{ChatID: body.ChatID, Text: body.Text, QuotedMessageID: body.QuotedMessageID, Key: r.Header.Get("Idempotency-Key"), Mentions: body.Mentions})
		if f.failWA == len(f.waCalls) {
			w.WriteHeader(429)
			fmt.Fprint(w, `{"retryAfterSeconds":60}`)
			return
		}
		if f.waEntered != nil {
			close(f.waEntered)
			<-f.waRelease
		}
		fmt.Fprintf(w, `{"messageId":"out-%d","timestamp":1700000000}`, len(f.waCalls))
	}))
	t.Cleanup(waServer.Close)
	master, err := tg.New(tgServer.URL, "master-fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	clients := map[string]*tg.Client{"master": master}
	for _, id := range []string{"200", "201"} {
		clients[id], err = tg.New(tgServer.URL, "child-"+id, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	routes, err := conversation.New(f.store, master, -100, 123)
	if err != nil {
		t.Fatal(err)
	}
	whatsapp, err := wa.New(waServer.URL, "fixture-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	var store relay.Store = f.store
	if wrap != nil {
		store = wrap(f.store)
	}
	options := relay.Options{SessionID: "fixture-session", OwnerID: 123, ManagementChatID: -100}
	if len(withMentions) > 0 && withMentions[0] {
		options.Mentions, err = mentions.New(f.store, whatsapp, master, "fixture-session")
		if err != nil {
			t.Fatal(err)
		}
	}
	f.service, err = relay.New(store, routes, func(_ context.Context, _ string, id string) (relay.Telegram, error) {
		if c := clients[id]; c != nil {
			return c, nil
		}
		return nil, delivery.ErrInvalid
	}, whatsapp, options)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *relayFixture) route(t *testing.T, waChat string, chat, thread int64) {
	t.Helper()
	if err := f.store.PutIngressRoute(t.Context(), "fixture-account", ingress.Route{WhatsAppChat: waChat, TelegramChatID: chat, ThreadID: thread}); err != nil {
		t.Fatal(err)
	}
}
func (f *relayFixture) enqueue(t *testing.T, kind, key, stream, bot string) int64 {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"bot": bot})
	_, err := f.store.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Kind: kind, Dedup: key, Stream: stream, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func (f *relayFixture) run(t *testing.T, id int64, kind string, h delivery.Handler) delivery.Report {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reports := make(chan delivery.Report, 1)
	w, err := delivery.NewWorker(f.store, map[string]delivery.Handler{kind: h}, delivery.WorkerOptions{Lease: 3 * time.Second, RenewEvery: 300 * time.Millisecond, StoreTimeout: 500 * time.Millisecond, PollEvery: 5 * time.Millisecond, ShutdownTimeout: time.Second, Report: func(r delivery.Report) {
		if r.JobID == id {
			reports <- r
			cancel()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	select {
	case r := <-reports:
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("relay job timeout")
		return delivery.Report{}
	}
}
func (f *relayFixture) incoming(t *testing.T, key string, m relay.Incoming) delivery.Report {
	id := f.enqueue(t, "wa", key, "wa:"+m.ChatID, "")
	return f.run(t, id, "wa", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		return f.service.IncomingText(ctx, j, e, m)
	})
}
func (f *relayFixture) outgoing(t *testing.T, key, bot string, m *models.Message) delivery.Report {
	stream := "tg:" + strconv.FormatInt(m.Chat.ID, 10) + ":0"
	if m.Chat.ID > 0 {
		stream = "tg:" + bot + ":" + strconv.FormatInt(m.Chat.ID, 10) + ":0"
	}
	id := f.enqueue(t, "tg", key, stream, bot)
	return f.run(t, id, "tg", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		return f.service.OutgoingText(ctx, j, e, relay.Outgoing{BotID: bot, Message: m})
	})
}
func ownerText(id int, chat int64, text string) *models.Message {
	kind := models.ChatTypeSupergroup
	if chat > 0 {
		kind = models.ChatTypePrivate
	}
	return &models.Message{ID: id, From: &models.User{ID: 123}, Chat: models.Chat{ID: chat, Type: kind}, Text: text, Date: 1700000000}
}

func TestIncomingTextFreezesFormattingDestinationAndCompletedParts(t *testing.T) {
	f := newRelayFixture(t, nil)
	f.route(t, "peer@c.us", -200, 0)
	f.failTG = 2
	m := relay.Incoming{ChatID: "peer@c.us", MessageID: "long", SenderID: "12025550123@c.us", Name: "Alice <b>", Body: strings.Repeat("😀", 4000)}
	r := f.incoming(t, "ow:long", m)
	if r.State != delivery.Pending || r.Delay != time.Minute {
		t.Fatal(r)
	}
	f.mu.Lock()
	if len(f.tgCalls) != 2 || f.tgCalls[0].ParseMode != "" || !strings.HasPrefix(f.tgCalls[0].Text, "Alice <b>: ") || !strings.Contains(f.tgCalls[0].Preview, "true") {
		t.Fatal(f.tgCalls)
	}
	originalSecond := f.tgCalls[1]
	f.mu.Unlock()
	f.route(t, "peer@c.us", -300, 7)
	m.Name = "Changed"
	m.Body = "Changed"
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	r = f.run(t, r.JobID, "wa", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		return f.service.IncomingText(ctx, j, e, m)
	})
	if r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tgCalls) != 4 || f.tgCalls[2].Text != originalSecond.Text || f.tgCalls[2].Chat != -200 || f.tgCalls[3].Chat != -200 || f.tgCalls[2].Thread != 0 {
		t.Fatal("frozen plan or completed part changed", len(f.tgCalls))
	}
	var n int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_mappings").Scan(&n); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	if f.reads.Load() != 0 {
		t.Fatal("incoming relay marked read")
	}
}

func TestWholeGroupQuotesAndOutgoingReadAreIndependent(t *testing.T) {
	f := newRelayFixture(t, nil)
	f.route(t, "group@g.us", -200, 0)
	if r := f.incoming(t, "ow:one", relay.Incoming{ChatID: "group@g.us", MessageID: "one", SenderID: "unknown@lid", Name: "Alice", Body: "Hello"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.incoming(t, "ow:two", relay.Incoming{ChatID: "group@g.us", MessageID: "two", SenderID: "unknown@lid", Name: "Alice", Body: "Reply", QuotedMessageID: "one"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	if len(f.tgCalls) != 2 || !strings.Contains(f.tgCalls[1].Reply, `"message_id":1001`) {
		t.Fatal("quote missing", f.tgCalls)
	}
	f.mu.Unlock()
	m := ownerText(7, -200, "Owner reply")
	m.MessageThreadID = 999
	m.ReplyToMessage = &models.Message{ID: 1001}
	r := f.outgoing(t, "tg:master:1", "master", m)
	if r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	if len(f.waCalls) != 1 || f.waCalls[0].QuotedMessageID != "one" || f.waCalls[0].ChatID != "group@g.us" {
		t.Fatal(f.waCalls)
	}
	sum := sha256.Sum256([]byte("send:-200:7"))
	if f.waCalls[0].Key != hex.EncodeToString(sum[:]) {
		t.Fatal("legacy stable key changed")
	}
	f.mu.Unlock()
	if f.reads.Load() != 0 {
		t.Fatal("send performed synchronous read")
	}
	var readID int64
	if err := f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup='management:send:-200:7:read'").Scan(&readID); err != nil {
		t.Fatal(err)
	}
	f.readFailure.Store(true)
	r = f.run(t, readID, "management", f.service.MarkRead)
	if r.State != delivery.Pending {
		t.Fatal(r)
	}
	f.mu.Lock()
	if len(f.waCalls) != 1 {
		t.Fatal("read failure replayed owner send")
	}
	f.mu.Unlock()
	if r := f.incoming(t, "ow:echo", relay.Incoming{ChatID: "group@g.us", MessageID: "out-1", SenderID: "peer@c.us", Own: true, Body: "Owner reply"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	if len(f.tgCalls) != 2 {
		t.Fatal("outgoing echo was relayed")
	}
	f.mu.Unlock()
}

func TestForeignConversationAndWrongOwnerCannotSend(t *testing.T) {
	f := newRelayFixture(t, nil)
	f.route(t, "first@g.us", -200, 44)
	if r := f.incoming(t, "ow:first", relay.Incoming{ChatID: "first@g.us", MessageID: "one", SenderID: "peer@lid", Name: "Alice", Body: "Hello"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.route(t, "first@g.us", -300, 0)
	f.route(t, "second@g.us", -200, 44)
	m := ownerText(7, -200, "Wrong conversation")
	m.MessageThreadID = 44
	m.ReplyToMessage = &models.Message{ID: 1001}
	if r := f.outgoing(t, "tg:foreign", "master", m); r.State != delivery.Failed {
		t.Fatal(r)
	}
	m = ownerText(8, -400, "Wrong owner")
	m.From.ID = 456
	if r := f.outgoing(t, "tg:other-owner", "master", m); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.waCalls) != 0 {
		t.Fatal("unauthorized/foreign reply sent")
	}
}

func (f *relayFixture) bind(t *testing.T, bot, contact string) {
	t.Helper()
	if err := f.store.PutIngressBot(t.Context(), "fixture-account", ingress.Bot{ID: bot}); err != nil {
		t.Fatal(err)
	}
	id := f.enqueue(t, "tg", "bind:"+bot, "binding:"+bot, "master")
	r := f.run(t, id, "tg", func(ctx context.Context, j delivery.Job, _ *delivery.Effects) error {
		if err := f.store.BindContactBot(ctx, j, contact, bot); err != nil {
			return err
		}
		return f.store.StartContactBot(ctx, j, bot)
	})
	if r.State != delivery.Done {
		t.Fatal(r)
	}
}
func TestPrivateBotsKeepOverlappingMessageIDsAndQuotesSeparate(t *testing.T) {
	f := newRelayFixture(t, nil)
	f.perBotIDs = true
	f.bind(t, "200", "first@c.us")
	f.bind(t, "201", "second@c.us")
	for _, c := range []struct{ chat, id string }{{"first@c.us", "first"}, {"second@c.us", "second"}} {
		if r := f.incoming(t, "ow:"+c.id, relay.Incoming{ChatID: c.chat, MessageID: c.id, SenderID: c.chat, Name: "Should be omitted", Body: "Hello"}); r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	m := ownerText(8, 123, "Reply to second")
	m.ReplyToMessage = &models.Message{ID: 7}
	if r := f.outgoing(t, "tg:201:1", "201", m); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.waCalls) != 1 || f.waCalls[0].ChatID != "second@c.us" || f.waCalls[0].QuotedMessageID != "second" {
		t.Fatal(f.waCalls)
	}
	if len(f.tgCalls) != 2 || f.tgCalls[0].Text != "Hello" || f.tgCalls[1].Text != "Hello" {
		t.Fatal("member-bot sender header leaked", f.tgCalls)
	}
	if f.topics.Load() != 0 {
		t.Fatal("started bot created a topic")
	}
}

type relayMappingFailure struct {
	*Store
	fail atomic.Bool
}

func (s *relayMappingFailure) RecordRelay(ctx context.Context, j delivery.Job, key string, part int) error {
	if strings.HasPrefix(key, "send:") && s.fail.CompareAndSwap(true, false) {
		return errors.New("fixture local write failure")
	}
	return s.Store.RecordRelay(ctx, j, key, part)
}
func TestEchoSuppressionUsesReceiptBeforeMappingAndRetryDoesNotResend(t *testing.T) {
	var wrapper *relayMappingFailure
	f := newRelayFixture(t, func(s *Store) relay.Store {
		wrapper = &relayMappingFailure{Store: s}
		wrapper.fail.Store(true)
		return wrapper
	})
	f.route(t, "peer@c.us", -200, 0)
	m := ownerText(7, -200, "Original")
	r := f.outgoing(t, "tg:first", "master", m)
	if r.State != delivery.Pending {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp()+interval '10 minutes' WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	echo, err := f.store.RelayEcho(t.Context(), "fixture-account", "peer@c.us", "out-1")
	if err != nil || !echo.Recognized {
		t.Fatal("confirmed unmapped receipt not recognized", echo, err)
	}
	var n int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_mappings").Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if report := f.incoming(t, "ow:early-echo", relay.Incoming{ChatID: "peer@c.us", MessageID: "out-1", SenderID: "peer@c.us", Own: true, Body: "Original"}); report.State != delivery.Done {
		t.Fatal(report)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	m.Text = "Changed input"
	r = f.run(t, r.JobID, "tg", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		return f.service.OutgoingText(ctx, j, e, relay.Outgoing{BotID: "master", Message: m})
	})
	if r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.waCalls) != 1 || f.waCalls[0].Text != "Original" || len(f.tgCalls) != 0 {
		t.Fatal(f.waCalls, f.tgCalls)
	}
}

func TestMutedRetryClosesOnlyUnsentParts(t *testing.T) {
	f := newRelayFixture(t, nil)
	f.route(t, "peer@c.us", -200, 0)
	f.failTG = 2
	m := relay.Incoming{ChatID: "peer@c.us", MessageID: "paused", SenderID: "peer@lid", Name: "Alice", Body: strings.Repeat("x", 8000)}
	r := f.incoming(t, "ow:paused", m)
	if r.State != delivery.Pending {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET mode='muted'; UPDATE jobs SET due=clock_timestamp() WHERE state='pending'"); err != nil {
		t.Fatal(err)
	}
	r = f.run(t, r.JobID, "wa", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		return f.service.IncomingText(ctx, j, e, m)
	})
	if r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	if len(f.tgCalls) != 2 {
		t.Fatal("muted retry emitted more parts")
	}
	f.mu.Unlock()
	var done, discarded int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FILTER(WHERE state='done'),count(*) FILTER(WHERE state='discarded') FROM effects").Scan(&done, &discarded); err != nil || done != 1 || discarded != 2 {
		t.Fatal(done, discarded, err)
	}
	plan, found, err := f.store.RelayPlan(t.Context(), "fixture-account", relay.IncomingKey(m.ChatID, m.MessageID))
	if err != nil || !found || !plan.Complete {
		t.Fatal(found, err)
	}
}

func TestPhoneOriginatedTextNeverLinksYouToThePeer(t *testing.T) {
	f := newRelayFixture(t, nil)
	f.route(t, "12025550123@c.us", -200, 0)
	r := f.incoming(t, "ow:phone", relay.Incoming{ChatID: "12025550123@c.us", MessageID: "phone", SenderID: "12025550123@c.us", Own: true, Body: "Mine"})
	if r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.HasPrefix(f.tgCalls[0].Text, "You · sent from WhatsApp: Mine") || strings.Contains(f.tgCalls[0].Entities, "https://wa.me/12025550123") {
		t.Fatal(f.tgCalls[0])
	}
}

func TestActiveSendDefersEchoAndCannotBeDiscardedByPause(t *testing.T) {
	f := newRelayFixture(t, nil)
	f.route(t, "peer@c.us", -200, 0)
	f.waEntered = make(chan struct{})
	f.waRelease = make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(f.waRelease) })
	owner := ownerText(7, -200, "Owner text")
	id := f.enqueue(t, "tg", "tg:active", "tg:-200:0", "master")
	done := make(chan delivery.Report, 1)
	go func() {
		done <- f.run(t, id, "tg", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
			return f.service.OutgoingText(ctx, j, e, relay.Outgoing{BotID: "master", Message: owner})
		})
	}()
	select {
	case <-f.waEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("send never started")
	}
	echo, err := f.store.RelayEcho(t.Context(), "fixture-account", "peer@c.us", "out-1")
	if err != nil || echo.Recognized || !echo.Pending {
		t.Fatal(echo, err)
	}
	m := relay.Incoming{ChatID: "peer@c.us", MessageID: "out-1", SenderID: "peer@c.us", Own: true, Body: "Owner text"}
	deferred := f.incoming(t, "ow:active-echo", m)
	if deferred.State != delivery.Pending {
		t.Fatal("active echo not deferred", deferred)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET mode='muted'"); err != nil {
		t.Fatal(err)
	}
	var token string
	if err := f.store.pool.QueryRow(t.Context(), "SELECT claim_token FROM jobs WHERE id=$1", id).Scan(&token); err != nil {
		t.Fatal(err)
	}
	job := delivery.Job{ID: id, Token: token, Event: delivery.Event{AccountID: "fixture-account"}}
	if err := f.store.CancelRelay(t.Context(), job, "send:-200:7"); !errors.Is(err, delivery.ErrUncertain) {
		t.Fatal("active effect was discarded", err)
	}
	release.Do(func() { close(f.waRelease) })
	if r := <-done; r.State != delivery.Done {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET mode='inbox'"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", deferred.JobID); err != nil {
		t.Fatal(err)
	}
	r := f.run(t, deferred.JobID, "wa", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		return f.service.IncomingText(ctx, j, e, m)
	})
	if r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.waCalls) != 1 || len(f.tgCalls) != 0 {
		t.Fatal("echo loop or resend", len(f.waCalls), len(f.tgCalls))
	}
}

func TestDeletedAndWrongTopicCopiesAreNotUsedAsQuotes(t *testing.T) {
	f := newRelayFixture(t, nil)
	f.route(t, "peer@c.us", -100, 44)
	if r := f.incoming(t, "ow:quoted", relay.Incoming{ChatID: "peer@c.us", MessageID: "quoted", SenderID: "peer@lid", Name: "Alice", Body: "Original"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if _, ok, err := f.store.WhatsAppQuote(t.Context(), "fixture-account", "peer@c.us", "quoted", "master", -100, 45); err != nil || ok {
		t.Fatal("foreign topic quote", ok, err)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE telegram_observations SET deleted=true WHERE message_id=1001"); err != nil {
		t.Fatal(err)
	}
	if r := f.incoming(t, "ow:no-quote", relay.Incoming{ChatID: "peer@c.us", MessageID: "later", SenderID: "peer@lid", Name: "Alice", Body: "Later", QuotedMessageID: "quoted"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tgCalls) != 2 || f.tgCalls[1].Reply != "" {
		t.Fatal("cleared copy was quoted", f.tgCalls)
	}
}

func TestUnsupportedTextCapabilitiesDoNotPartiallyForward(t *testing.T) {
	for _, text := range []string{"/history 50", "@PeerBot hello", "@{Alice|fixture}"} {
		t.Run(text, func(t *testing.T) {
			f := newRelayFixture(t, nil)
			f.route(t, "peer@c.us", -200, 0)
			if r := f.outgoing(t, "tg:unsupported", "master", ownerText(7, -200, text)); r.State != delivery.Failed {
				t.Fatal(r)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.waCalls) != 0 || len(f.tgCalls) != 0 {
				t.Fatal("unsupported content partially forwarded")
			}
		})
	}
}
