package postgres

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/attachments"
	"github.com/veryshyjelly/bifrost/internal/bridge"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/history"
	wa "github.com/veryshyjelly/bifrost/internal/providers/openwa"
	tg "github.com/veryshyjelly/bifrost/internal/providers/telegram"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

type nativeCall struct {
	Method, Bot, Text string
	Chat, Thread      int64
	Body              []byte
	Reply             string
}
type nativeSend struct {
	Method, Key string
	Request     map[string]json.RawMessage
}
type nativeFixture struct {
	f              *relayFixture
	native         *relay.Native
	assets         *attachments.Assets
	mu             sync.Mutex
	data           []byte
	calls          []nativeCall
	reads          int
	failMethod     string
	failCode       int
	dropMethod     string
	sends          []nativeSend
	fileReads      int
	sendFailure    int
	dropSend       bool
	sendFailMethod string
	outbound       *relay.Outbound
}

func newNativeFixture(t *testing.T, f *relayFixture) *nativeFixture {
	t.Helper()
	if f == nil {
		f = newRelayFixture(t, nil, true)
	}
	n := &nativeFixture{f: f}
	var imageBytes bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&imageBytes, img); err != nil {
		t.Fatal(err)
	}
	n.data = imageBytes.Bytes()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.mu.Lock()
		defer n.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/file/") {
			n.fileReads++
			w.Write(n.data)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Method == http.MethodPost {
				var request map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				n.sends = append(n.sends, nativeSend{Method: r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], Key: r.Header.Get("Idempotency-Key"), Request: request})
				if n.dropSend {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
					} else {
						conn.Close()
					}
					return
				}
				if n.sendFailure != 0 && (n.sendFailMethod == "" || n.sendFailMethod == n.sends[len(n.sends)-1].Method) {
					w.WriteHeader(n.sendFailure)
					fmt.Fprint(w, `{"message":"fixture send refusal"}`)
					return
				}
				fmt.Fprint(w, `{"messageId":"out-native","timestamp":1700000000}`)
				return
			}
			n.reads++
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("X-Media-Mimetype", "image/png")
			w.Header().Set("Content-Disposition", `attachment; filename="original.png"`)
			w.Write(n.data)
			return
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
		} else if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if method == "getFile" {
			fmt.Fprint(w, `{"ok":true,"result":{"file_id":"fixture-file","file_unique_id":"fixture-unique","file_path":"fixture/file.bin"}}`)
			return
		}
		botID := strings.Split(strings.TrimPrefix(r.URL.Path, "/bot"), "/")[0]
		chat, _ := strconv.ParseInt(r.FormValue("chat_id"), 10, 64)
		thread, _ := strconv.ParseInt(r.FormValue("message_thread_id"), 10, 64)
		call := nativeCall{Method: method, Bot: botID, Chat: chat, Thread: thread, Text: r.FormValue("text"), Reply: r.FormValue("reply_parameters")}
		if call.Text == "" {
			call.Text = r.FormValue("caption")
		}
		if r.MultipartForm != nil {
			for _, files := range r.MultipartForm.File {
				for _, header := range files {
					file, err := header.Open()
					if err != nil {
						t.Error(err)
						continue
					}
					body, err := io.ReadAll(file)
					file.Close()
					if err != nil {
						t.Error(err)
					}
					call.Body = append(call.Body, body...)
				}
			}
		}
		n.calls = append(n.calls, call)
		if method == n.dropMethod {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
			} else {
				conn.Close()
			}
			return
		}
		if method == n.failMethod {
			fmt.Fprintf(w, `{"ok":false,"error_code":%d,"description":"fixture native refusal","parameters":{"retry_after":1}}`, n.failCode)
			return
		}
		result := map[string]any{"message_id": 2000 + len(n.calls), "date": 1700000000, "message_thread_id": thread, "chat": map[string]any{"id": chat, "type": "supergroup"}}
		if method == "sendPhoto" {
			result["photo"] = []map[string]any{{"file_id": "photo-file", "file_unique_id": "photo-unique", "width": 8, "height": 8}}
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
	}))
	t.Cleanup(server.Close)
	client, err := wa.New(server.URL, "fixture-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	n.assets, err = attachments.NewAssets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clients := map[string]*tg.Client{}
	for _, id := range []string{"master", "200", "201"} {
		clients[id], err = tg.New(server.URL, id, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	sources, err := attachments.NewSources(n.assets, client, func(_ context.Context, _ string, id string) (attachments.Telegram, error) { return clients[id], nil }, "fixture-session", t.TempDir(), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := attachments.NewCodec(n.assets, attachments.CodecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n.native, err = relay.NewNative(f.store, f.routing, func(_ context.Context, _ string, id string) (relay.NativeTelegram, error) { return clients[id], nil }, n.assets, sources, codec, relay.NativeOptions{Session: "fixture-session", OwnerID: 123, Management: -100, MaxBytes: 8 << 20, Identity: f.whatsapp, TextResume: f.service, Mentions: f.mentionService})
	if err != nil {
		t.Fatal(err)
	}
	options := f.dispatchOptions
	options.Extensions.WhatsApp = map[sdk.WebhookEvent]delivery.Handler{sdk.EventMessageReceived: n.native.Incoming, sdk.EventMessageSent: n.native.Incoming}
	replies, err := control.New(f.store, f.replyClients, -100)
	if err != nil {
		t.Fatal(err)
	}
	outbound, err := relay.NewOutbound(f.store, client, n.assets, sources, codec, replies, relay.Options{SessionID: "fixture-session", OwnerID: 123, ManagementChatID: -100, Mentions: f.mentionService})
	if err != nil {
		t.Fatal(err)
	}
	options.Extensions.Telegram = outbound.Handle
	n.outbound = outbound
	f.dispatcher, err = bridge.New(f.store, f.service, f.whatsapp, options)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestNativeOutgoingStickerRefusalUsesFrozenOriginalAndDistinctKey(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	m := pickerMessage(101, -100, 44, "")
	m.Sticker = &models.Sticker{FileID: "fixture-file", FileUniqueID: "sticker", Width: 512, Height: 512}
	if !admitPicker(t, n.f, "master", models.Update{ID: 101, Message: m}) {
		t.Fatal("not admitted")
	}
	var jobID int64
	n.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup='tg:master:101'").Scan(&jobID)
	asset, err := n.assets.Save(t.Context(), bytes.NewReader(n.data), "original.tgs", "application/x-tgsticker", 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	n.sendFailure = 400
	n.sendFailMethod = "send-sticker"
	if r := n.f.run(t, jobID, "tg", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		route, _, err := n.f.store.OutgoingRoute(ctx, j, "master", -100, 44, -100)
		if err != nil {
			return err
		}
		key := relay.OutgoingKey("master", -100, 101)
		// Conversion bytes are tested by the real codec suite. Here both immutable
		// references exercise refusal/recovery without starting an external codec.
		p := relay.Plan{Key: key, Prefix: key, Direction: "out", WhatsAppChat: "peer@g.us", BotID: "master", TelegramChat: -100, TelegramMessage: 101, ThreadID: 44, Route: route, Outgoing: &relay.WhatsAppRequest{ChatID: "peer@g.us", Kind: "sticker", Asset: &asset}, OutgoingFallback: &relay.WhatsAppRequest{ChatID: "peer@g.us", Kind: "document", Asset: &asset}, SourceAsset: &asset}
		if _, err := n.f.store.PrepareRelay(ctx, j, p); err != nil {
			return err
		}
		return n.outbound.Handle(ctx, j, e, "master", &models.Update{ID: 101, Message: m})
	}); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.sends) != 2 || n.sends[0].Method != "send-sticker" || n.sends[1].Method != "send-document" || n.sends[0].Key == n.sends[1].Key {
		t.Fatal(n.sends)
	}
	var discarded int
	n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM effects WHERE state='discarded'").Scan(&discarded)
	if discarded != 1 {
		t.Fatal(discarded)
	}
	echo, err := n.f.store.RelayEcho(t.Context(), "fixture-account", "peer@g.us", "out-native")
	if err != nil || !echo.Recognized {
		t.Fatal(echo, err)
	}
}

func TestNativeOutgoingAttachmentRetryFreezesFileAndSeparatesReadReceipt(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	m := pickerMessage(101, -100, 44, "")
	m.Caption = "literal <caption>"
	m.Document = &models.Document{FileID: "fixture-file", FileUniqueID: "fixture-unique", FileName: "fixture.png", MimeType: "image/png"}
	u := models.Update{ID: 101, Message: m}
	if !admitPicker(t, n.f, "master", u) {
		t.Fatal("source not admitted")
	}
	n.sendFailure = 429
	if r := n.f.dispatch(t, "tg:master:101", "tg"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	n.mu.Lock()
	original := append([]byte(nil), n.data...)
	n.data = []byte("do not fetch again")
	n.sendFailure = 0
	n.mu.Unlock()
	n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE dedup='tg:master:101'")
	if r := n.f.dispatch(t, "tg:master:101", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.sends) != 2 || n.sends[0].Method != "send-document" || n.fileReads != 1 || n.sends[0].Key == "" || n.sends[0].Key != n.sends[1].Key {
		t.Fatal(n.sends, n.fileReads)
	}
	var encoded, caption string
	json.Unmarshal(n.sends[1].Request["base64"], &encoded)
	json.Unmarshal(n.sends[1].Request["caption"], &caption)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || !bytes.Equal(original, data) || caption != m.Caption {
		t.Fatal("frozen attachment or caption changed", err, caption)
	}
	var effects, reads int
	n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM effects WHERE state='done'").Scan(&effects)
	n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='management' AND payload->>'type'='mark_read' AND state='pending'").Scan(&reads)
	if effects != 1 || reads != 1 {
		t.Fatal("send/read were coupled", effects, reads)
	}
	asset, found, err := n.f.store.ArchivedAsset(t.Context(), "fixture-account", "peer@g.us", "out-native")
	if err != nil || !found {
		t.Fatal(found, err)
	}
	file, err := n.assets.Open(t.Context(), asset)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
}

func TestNativeOutgoingProviderTypesAndUncertainSend(t *testing.T) {
	for _, kind := range []string{"voice", "location", "contact", "poll", "uncertain"} {
		t.Run(kind, func(t *testing.T) {
			n := newNativeFixture(t, nil)
			n.f.route(t, "peer@g.us", -100, 44)
			m := pickerMessage(101, -100, 44, "")
			method := ""
			switch kind {
			case "voice":
				m.Voice = &models.Voice{FileID: "fixture-file", FileUniqueID: "voice-unique", MimeType: "audio/ogg; codecs=opus"}
				method = "send-audio"
			case "location":
				m.Location = &models.Location{Latitude: 0, Longitude: 0}
				method = "send-location"
			case "contact":
				m.Contact = &models.Contact{PhoneNumber: "+12025550123", FirstName: "Fixture"}
				method = "send-contact"
			case "poll":
				m.Poll = &models.Poll{ID: "poll", Question: "Which?", Options: []models.PollOption{{Text: "A"}, {Text: "B"}}, AllowsMultipleAnswers: true}
				method = "send-poll"
			case "uncertain":
				m.Document = &models.Document{FileID: "fixture-file", FileUniqueID: "unique", FileName: "f.png", MimeType: "image/png"}
				method = "send-document"
				n.dropSend = true
			}
			if !admitPicker(t, n.f, "master", models.Update{ID: 101, Message: m}) {
				t.Fatal("not admitted")
			}
			expected := delivery.Done
			if kind == "uncertain" {
				expected = delivery.Uncertain
			}
			if r := n.f.dispatch(t, "tg:master:101", "tg"); r.State != expected {
				t.Fatal(r)
			}
			n.mu.Lock()
			defer n.mu.Unlock()
			if len(n.sends) != 1 || n.sends[0].Method != method {
				t.Fatal(n.sends)
			}
			if kind == "voice" && string(n.sends[0].Request["ptt"]) != "true" {
				t.Fatal("voice lost PTT", n.sends)
			}
			if kind == "location" && (string(n.sends[0].Request["latitude"]) != "0" || string(n.sends[0].Request["longitude"]) != "0") {
				t.Fatal("zero coordinates omitted", n.sends)
			}
		})
	}
}
func (n *nativeFixture) incoming(t *testing.T, id, body string) delivery.Report {
	t.Helper()
	m := sdk.ChatHistoryMessage{ID: id, ChatID: "peer@g.us", Author: "12345000000001@lid", Body: body, Type: sdk.MsgImage, IsGroup: true, Timestamp: 1700000000, Media: &sdk.ChatHistoryMedia{Mimetype: "image/png"}}
	raw, _ := json.Marshal(map[string]any{"event": "message.received", "sessionId": "fixture-session", "idempotencyKey": id, "data": m})
	if !fixtureIngress(t, n.f)("openwa", raw) {
		t.Fatal("media source not admitted")
	}
	return n.f.dispatch(t, "ow:"+id, "wa")
}
func TestNativeIncomingAttachmentUsesFrozenSDKUploadAndScopedMapping(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	if r := n.incoming(t, "native-image", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.reads != 1 || len(n.calls) != 1 || n.calls[0].Method != "sendPhoto" || !bytes.Equal(n.calls[0].Body, n.data) || !strings.Contains(n.calls[0].Text, "Caption") {
		t.Fatal(n.reads, n.calls)
	}
	mapped, found, err := n.f.store.TelegramMapping(t.Context(), "fixture-account", "master", -100, 2001)
	if err != nil || !found || mapped.WhatsAppMessage != "native-image" || mapped.ThreadID != 44 {
		t.Fatal(mapped, found, err)
	}
	var expiry int64
	if err := n.f.store.pool.QueryRow(t.Context(), "SELECT expires_at FROM telegram_observations WHERE message_id=2001").Scan(&expiry); err != nil || expiry != 0 {
		t.Fatal("attachment was scheduled for expiry", expiry, err)
	}
}
func TestNativeAttachmentRetryUsesFrozenBytesWithoutAnotherLookup(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	n.failMethod, n.failCode = "sendPhoto", 429
	first := n.incoming(t, "native-retry", "Caption")
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	n.mu.Lock()
	original := append([]byte(nil), n.data...)
	n.data = []byte("replacement source must not be read")
	n.failMethod = ""
	n.mu.Unlock()
	if _, err := n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", first.JobID); err != nil {
		t.Fatal(err)
	}
	if r := n.f.dispatch(t, "ow:native-retry", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.reads != 1 || len(n.calls) != 2 || !bytes.Equal(n.calls[1].Body, original) {
		t.Fatal("frozen upload changed", n.reads, len(n.calls))
	}
}
func TestNativeKnownPhotoRefusalRetainsCaptionAndOriginalDocument(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	n.failMethod, n.failCode = "sendPhoto", 400
	caption := strings.Repeat("x", 940)
	if r := n.incoming(t, "native-fallback", caption); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	var label, document bool
	for _, call := range n.calls {
		if call.Method == "sendMessage" && strings.Contains(call.Text, caption) {
			label = true
		}
		if call.Method == "sendDocument" && bytes.Equal(call.Body, n.data) {
			document = true
		}
	}
	if !label || !document {
		t.Fatal("fallback lost caption or original", label, document, n.calls)
	}
}
func TestNativeUnknownUploadDoesNotSendOriginalFallback(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	n.dropMethod = "sendPhoto"
	if r := n.incoming(t, "native-unknown", "Caption"); r.State != delivery.Uncertain {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 1 || n.calls[0].Method != "sendPhoto" {
		t.Fatal("unknown upload retried", n.calls)
	}
}

func TestHistoryNativeBatchPreservesFreshQuotesMediaAndDailyHeaders(t *testing.T) {
	h := newHistoryFixture(t)
	n := newNativeFixture(t, h.f)
	wireNativeHistory(t, h, n)
	h.batch.Store([]sdk.ChatHistoryMessage{{ID: "child", ChatID: "peer@g.us", Author: "12345000000001@lid", Body: "Child image", Timestamp: 1700000001, Type: sdk.MsgImage, Media: &sdk.ChatHistoryMedia{Mimetype: "image/png"}, QuotedMessage: &sdk.QuotedMessage{ID: "parent", Body: "Parent"}}, {ID: "parent", ChatID: "peer@g.us", Author: "12345000000001@lid", Body: "Parent", Timestamp: 1700000002, Type: sdk.MsgText}})
	choice, message := h.prompt(t, "/history")
	if r := h.update(t, historyCallback(101, choice, "all", message, 44)); r.State != delivery.Done {
		t.Fatal(r)
	}
	h.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp()+interval '1 hour' WHERE kind='control-expire' AND state='pending'")
	var fetch int64
	h.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='history'").Scan(&fetch)
	if r := h.f.run(t, fetch, "history", h.service.Fetch); r.State != delivery.Done {
		t.Fatal(r)
	}
	rows, err := h.f.store.pool.Query(t.Context(), "SELECT job_id FROM history_items ORDER BY item_index")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if r := h.f.run(t, id, "history-message", h.service.Item); r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	var finishID int64
	h.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='history-finish'").Scan(&finishID)
	if r := h.f.run(t, finishID, "history-finish", h.service.Finish); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 3 || n.calls[0].Method != "sendMessage" || n.calls[1].Method != "sendMessage" || n.calls[2].Method != "sendPhoto" || n.reads != 1 {
		t.Fatal(n.calls, n.reads)
	}
	var reply models.ReplyParameters
	if json.Unmarshal([]byte(n.calls[2].Reply), &reply) != nil || reply.MessageID != 2002 {
		t.Fatal("history quote did not use fresh parent", n.calls[2].Reply)
	}
	var state string
	if err := h.f.store.pool.QueryRow(t.Context(), "SELECT state FROM history_runs").Scan(&state); err != nil || state != "complete" {
		t.Fatal(state, err)
	}
	var count int
	if err := h.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_mappings").Scan(&count); err != nil || count != 2 {
		t.Fatal("day header was mapped as source", count, err)
	}
}

func wireNativeHistory(t *testing.T, h *historyFixture, n *nativeFixture) {
	t.Helper()
	replies, err := control.New(h.f.store, h.f.replyClients, -100)
	if err != nil {
		t.Fatal(err)
	}
	h.service, err = history.New(h.f.store, h.reader, n.native, replies, h.settings)
	if err != nil {
		t.Fatal(err)
	}
}
func fetchNativeHistory(t *testing.T, h *historyFixture, mode string) []int64 {
	t.Helper()
	choice, message := h.prompt(t, "/history")
	if r := h.update(t, historyCallback(101, choice, mode, message, 44)); r.State != delivery.Done {
		t.Fatal(r)
	}
	h.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp()+interval '1 hour' WHERE kind='control-expire' AND state='pending'")
	var fetch int64
	if err := h.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='history'").Scan(&fetch); err != nil {
		t.Fatal(err)
	}
	if r := h.f.run(t, fetch, "history", h.service.Fetch); r.State != delivery.Done {
		t.Fatal(r)
	}
	rows, err := h.f.store.pool.Query(t.Context(), "SELECT job_id FROM history_items ORDER BY item_index")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	return ids
}
func TestHistorySkipResumesPartialNativePlanWithoutRepeatingLabel(t *testing.T) {
	h := newHistoryFixture(t)
	n := newNativeFixture(t, h.f)
	wireNativeHistory(t, h, n)
	n.failMethod = "sendPhoto"
	n.failCode = 429
	if r := n.incoming(t, "partial", strings.Repeat("caption", 200)); r.State != delivery.Pending {
		t.Fatal(r)
	}
	n.mu.Lock()
	if len(n.calls) != 2 {
		t.Fatal(n.calls)
	}
	n.failMethod = ""
	n.mu.Unlock()
	h.batch.Store([]sdk.ChatHistoryMessage{{ID: "partial", ChatID: "peer@g.us", Author: "12345000000001@lid", Type: sdk.MsgImage, Body: "changed source must not be rendered", Timestamp: 1700000000, Media: &sdk.ChatHistoryMedia{Mimetype: "image/png"}}})
	ids := fetchNativeHistory(t, h, "skip")
	if len(ids) != 1 {
		t.Fatal(ids)
	}
	if r := h.f.run(t, ids[0], "history-message", h.service.Item); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 3 || n.calls[2].Method != "sendPhoto" || n.reads != 1 {
		t.Fatal("partial import repeated label or source lookup", n.calls, n.reads)
	}
	var state string
	if err := h.f.store.pool.QueryRow(t.Context(), "SELECT state FROM jobs WHERE dedup='ow:partial'").Scan(&state); err != nil || state != "done" {
		t.Fatal(state, err)
	}
	var plans int
	h.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM relay_plans").Scan(&plans)
	if plans != 1 {
		t.Fatal("fresh plan replaced frozen delivery", plans)
	}
}
func TestHistorySkipDoesNotAdoptUnknownNativeSendAndReleasesLiveHold(t *testing.T) {
	h := newHistoryFixture(t)
	n := newNativeFixture(t, h.f)
	wireNativeHistory(t, h, n)
	n.dropMethod = "sendPhoto"
	if r := n.incoming(t, "unknown-old", "Caption"); r.State != delivery.Uncertain {
		t.Fatal(r)
	}
	n.mu.Lock()
	n.dropMethod = ""
	n.mu.Unlock()
	h.batch.Store([]sdk.ChatHistoryMessage{{ID: "unknown-old", ChatID: "peer@g.us", Author: "12345000000001@lid", Type: sdk.MsgImage, Timestamp: 1700000000, Media: &sdk.ChatHistoryMedia{Mimetype: "image/png"}}})
	ids := fetchNativeHistory(t, h, "skip")
	if r := h.f.run(t, ids[0], "history-message", h.service.Item); r.State != delivery.Uncertain {
		t.Fatal(r)
	}
	var state string
	if err := h.f.store.pool.QueryRow(t.Context(), "SELECT state FROM history_runs").Scan(&state); err != nil || state != "failed" {
		t.Fatal(state, err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 1 || n.reads != 1 {
		t.Fatal("unknown outcome was resent", n.calls, n.reads)
	}
}
func TestHistorySkipCompletedDestinationEmitsNoEmptyDayHeader(t *testing.T) {
	h := newHistoryFixture(t)
	n := newNativeFixture(t, h.f)
	wireNativeHistory(t, h, n)
	if r := n.incoming(t, "delivered", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	h.batch.Store([]sdk.ChatHistoryMessage{{ID: "delivered", ChatID: "peer@g.us", Author: "12345000000001@lid", Body: "Caption", Type: sdk.MsgImage, Timestamp: 1700000000, Media: &sdk.ChatHistoryMedia{Mimetype: "image/png"}}})
	ids := fetchNativeHistory(t, h, "skip")
	if r := h.f.run(t, ids[0], "history-message", h.service.Item); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 1 {
		t.Fatal("completed destination emitted copies or heading", n.calls)
	}
	var state string
	if err := h.f.store.pool.QueryRow(t.Context(), "SELECT state FROM history_items").Scan(&state); err != nil || state != "skipped" {
		t.Fatal(state, err)
	}
}
