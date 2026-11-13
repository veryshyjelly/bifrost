package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres"
)

func runtimeTestStore(t *testing.T) (*postgres.Store, *pgxpool.Pool) {
	return runtimeTestStoreForAccount(t, "fixture-account")
}

func runtimeTestStoreForAccount(t *testing.T, account string) (*postgres.Store, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("BIFROST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal("invalid test database")
	}
	t.Cleanup(admin.Close)
	schema := "runtime_test_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(t.Context(), "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid test database")
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	store, err := postgres.Open(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	view, err := pgxpool.New(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	if err = store.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.InitializeIdentity(t.Context(), account, [32]byte{}); err != nil {
		t.Fatal(err)
	}
	return store, view
}

func runRuntimeJob(t *testing.T, store *postgres.Store, handlers map[string]delivery.Handler) delivery.Report {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reports := make(chan delivery.Report, 1)
	w, err := delivery.NewWorker(store, handlers, delivery.WorkerOptions{Concurrency: 1, Lease: 9 * time.Second, RenewEvery: time.Second, StoreTimeout: 2 * time.Second, ShutdownTimeout: 2 * time.Second, PollEvery: 10 * time.Millisecond, Report: func(r delivery.Report) {
		select {
		case reports <- r:
		default:
		}
		cancel()
	}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	var report delivery.Report
	select {
	case report = <-reports:
	case <-time.After(20 * time.Second):
		t.Fatal("composed worker produced no report")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("composed worker did not stop")
	}
	if report.State != delivery.Done {
		t.Fatal("composed workflow failed", report)
	}
	return report
}

func TestComposedRuntimeBootstrapsAndRelaysThroughNativeProviders(t *testing.T) {
	store, view := runtimeTestStore(t)
	var sends, topics, logouts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		switch method {
		case "logOut", "logout":
			logouts.Add(1)
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		case "getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":999,"is_bot":true,"username":"fixture_master","first_name":"Fixture"}}`))
		case "setWebhook", "setMyCommands", "setChatMenuButton":
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		case "createForumTopic":
			topics.Add(1)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_thread_id":44,"name":"Fixture group"}}`))
		case "sendMessage":
			id := 1000 + sends.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": id, "message_thread_id": 44, "date": time.Now().Unix(), "chat": map[string]any{"id": -100, "type": "supergroup"}, "from": map[string]any{"id": 999, "is_bot": true}, "text": "Delivered"}})
		case "fixture-session":
			_, _ = w.Write([]byte(`{"id":"fixture-session","status":"ready","phone":"12025550999"}`))
		case "12345@g.us":
			_, _ = w.Write([]byte(`{"id":"12345@g.us","name":"Fixture group","participants":[]}`))
		default:
			t.Errorf("unexpected native provider method %s", method)
			http.Error(w, "unsupported fixture method", 500)
		}
	}))
	defer server.Close()
	c := runtimeServiceOptions{Account: "fixture-account", Session: "fixture-session", OpenWAURL: server.URL, OpenWAKey: "fixture", TelegramURL: server.URL, CloudURL: server.URL, MasterToken: "999:fixture-token", Callback: "http://bifrost.invalid:3210", AdminToken: strings.Repeat("x", 32), MediaRoot: t.TempDir(), TelegramRoot: t.TempDir(), Cron: "0 * * * *", Timezone: "UTC", Owner: 123, Management: -100, MaxBytes: 32 << 20, CommandTTL: 60, ControlTTL: 600}
	s, err := composeRuntimeServices(store, c)
	if err != nil {
		t.Fatal(err)
	}
	boot, stopBoot := context.WithTimeout(t.Context(), 20*time.Second)
	defer stopBoot()
	if err = s.bootstrapBots(boot, store, c.Account, c.MasterToken); err != nil {
		t.Fatal(err)
	}
	if logouts.Load() != 1 {
		t.Fatal("native bootstrap did not explicitly migrate the token")
	}
	payload := json.RawMessage(`{"accountId":"fixture-account","sessionId":"fixture-session","event":"message.received","idempotencyKey":"runtime-source","data":{"id":"source","chatId":"12345@g.us","author":"888888@lid","isGroup":true,"chatName":"Fixture group","body":"Hello from WhatsApp","type":"text","timestamp":1700000000}}`)
	event := delivery.Event{AccountID: c.Account, Dedup: "ow:runtime-source", Stream: "wa:12345@g.us", Kind: "wa", Payload: payload}
	if admitted, err := store.Enqueue(t.Context(), event); err != nil || !admitted {
		t.Fatal(err)
	}
	runRuntimeJob(t, store, s.Handlers)
	var copies int
	if err = view.QueryRow(t.Context(), "SELECT count(*) FROM message_mappings WHERE whatsapp_message_id='source'").Scan(&copies); err != nil || copies != 1 || topics.Load() != 1 || sends.Load() != 1 {
		t.Fatal("assembled runtime did not create and relay one copy", copies, topics.Load(), sends.Load(), err)
	}
	if admitted, err := store.Enqueue(t.Context(), event); err != nil || admitted {
		t.Fatal("duplicate ingress was admitted", err)
	}
}
