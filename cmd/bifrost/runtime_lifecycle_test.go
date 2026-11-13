package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/veryshyjelly/bifrost/internal/config"
	"github.com/veryshyjelly/bifrost/internal/postgres"
)

func TestRuntimeLifecycleBuffersIngressRechecksOnRestartAndStopsOnLostOwnership(t *testing.T) {
	account := "lifecycle-" + t.Name()
	store, view := runtimeTestStoreForAccount(t, account)
	if _, err := view.Exec(t.Context(), "INSERT INTO bridge_routes(account_id,whatsapp_chat,telegram_chat_id,thread_id) VALUES($1,'12345@g.us',-100,44)", account); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	callback := "http://" + listener.Addr().String()
	var sends, packets, logouts, checks, hookWrites atomic.Int32
	verified := make(chan struct{})
	release := make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			if r.MultipartForm != nil {
				defer r.MultipartForm.RemoveAll()
			}
		}
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		switch method {
		case "logout":
			logouts.Add(1)
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		case "getMe":
			if checks.Add(1) == 1 {
				close(verified)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":999,"is_bot":true,"username":"fixture_master","first_name":"Fixture"}}`))
		case "setWebhook", "setMyCommands", "setChatMenuButton":
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		case "fixture-session":
			_, _ = w.Write([]byte(`{"id":"fixture-session","status":"ready","phone":"12025550999"}`))
		case "status":
			_, _ = w.Write([]byte(`{"statuses":[]}`))
		case "webhooks":
			if r.Method == "GET" {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			hookWrites.Add(1)
			var request struct{ Events []string }
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "hook", "sessionId": "fixture-session", "url": callback + "/webhooks/openwa", "active": true, "events": request.Events})
		case "12345@g.us":
			_, _ = w.Write([]byte(`{"id":"12345@g.us","name":"Fixture group","participants":[]}`))
		case "createForumTopic":
			name := r.FormValue("name")
			thread := 44
			if strings.Contains(name, "Status") {
				thread = 45
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_thread_id": thread, "name": name}})
		case "sendMessage":
			if strings.Contains(r.FormValue("text"), "Buffered incoming message") {
				sends.Add(1)
			}
			id := 1000 + packets.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": id, "message_thread_id": 44, "date": time.Now().Unix(), "chat": map[string]any{"id": -100, "type": "supergroup"}, "from": map[string]any{"id": 999, "is_bot": true}, "text": "Delivered"}})
		default:
			t.Errorf("unexpected runtime provider method %s", method)
			http.Error(w, "unsupported", 500)
		}
	}))
	defer func() { unblock.Do(func() { close(release) }); provider.Close() }()
	c := config.Runtime{Config: config.Config{AccountID: account, AdminToken: strings.Repeat("a", 32), OpenWASession: "fixture-session", WebhookSecret: strings.Repeat("s", 32), OwnerID: 123, ManagementChatID: -100, MasterBotID: 999, CommandTTL: time.Minute, ControlTTL: 10 * time.Minute, SyncCron: "off", SyncTimezone: "UTC"}, OpenWAURL: provider.URL, OpenWAKey: "fixture", TelegramURL: provider.URL, CloudURL: provider.URL, MasterToken: "999:fixture-token", Callback: callback, MediaRoot: t.TempDir(), TelegramRoot: t.TempDir(), MaxBytes: 100 << 20, HistoryLimit: 20, DeliveryInterval: 100 * time.Millisecond}
	start := func(l net.Listener) (context.CancelFunc, <-chan error) {
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- serveRuntime(ctx, store, c, l, nil); close(done) }()
		t.Cleanup(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				t.Error("runtime did not stop")
			}
		})
		return cancel, done
	}
	cancel, done := start(listener)
	select {
	case <-verified:
	case <-time.After(20 * time.Second):
		t.Fatal("runtime never verified local bot")
	}
	response, err := http.Get(callback + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 503 {
		t.Fatal("runtime ready before verification")
	}
	body := `{"sessionId":"fixture-session","event":"message.received","idempotencyKey":"lifecycle-source","data":{"id":"source","chatId":"12345@g.us","author":"888888@lid","isGroup":true,"chatName":"Fixture group","body":"Buffered incoming message","type":"text","timestamp":1700000000}}`
	post := func(signed bool) int {
		t.Helper()
		req, err := http.NewRequest("POST", callback+"/webhooks/openwa", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if signed {
			mac := hmac.New(sha256.New, []byte(c.WebhookSecret))
			_, _ = mac.Write([]byte(body))
			req.Header.Set("X-OpenWA-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	if post(false) != 401 || post(true) != 200 || sends.Load() != 0 {
		t.Fatal("startup ingress authentication/buffering failed")
	}
	unblock.Do(func() { close(release) })
	waitRuntime := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			select {
			case err := <-done:
				t.Fatal("runtime stopped before test condition", err)
			default:
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("runtime condition did not complete")
	}
	waitRuntime(func() bool { return sends.Load() == 1 })
	waitRuntime(func() bool {
		r, e := http.Get(callback + "/readyz")
		if e != nil {
			return false
		}
		r.Body.Close()
		return r.StatusCode == 200
	})
	if post(true) != 200 {
		t.Fatal("duplicate ingress refused")
	}
	var ciphertext string
	if err = view.QueryRow(t.Context(), "SELECT ciphertext FROM telegram_credentials WHERE bot_id='master'").Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	second, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err = serveRuntime(t.Context(), store, c, second, nil); !errors.Is(err, postgres.ErrRuntimeHeld) {
		t.Fatal("second runtime was not refused", err)
	}
	second.Close()
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("runtime shutdown blocked")
	}
	// Cleanup can inspect a completed result without draining the only value.
	listener, err = net.Listen("tcp", strings.TrimPrefix(callback, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	_, restarted := start(listener)
	done = restarted
	waitRuntime(func() bool { return hookWrites.Load() == 2 })
	if logouts.Load() != 1 || checks.Load() < 3 {
		t.Fatal("restart replayed logout or skipped native verification")
	}
	var after string
	if err = view.QueryRow(t.Context(), "SELECT ciphertext FROM telegram_credentials WHERE bot_id='master'").Scan(&after); err != nil || after != ciphertext {
		t.Fatal("restart replaced original credential", err)
	}
	var copies int
	if err = view.QueryRow(t.Context(), "SELECT count(*) FROM message_mappings WHERE whatsapp_message_id='source'").Scan(&copies); err != nil || copies != 1 || sends.Load() != 1 {
		t.Fatal("restart replayed message", err)
	}
	if _, err = view.Exec(t.Context(), `SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype='advisory' AND granted AND classid=(hashtext('bifrost-runtime')::bigint & 4294967295)::oid AND objid=(hashtext($1)::bigint & 4294967295)::oid`, account); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-restarted:
		if !errors.Is(err, postgres.ErrRuntimeLeaseLost) {
			t.Fatal("lost ownership did not stop runtime", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runtime retained lost ownership")
	}
}
