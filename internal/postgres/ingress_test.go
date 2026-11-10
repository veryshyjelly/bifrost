package postgres

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/httpserver"
	"github.com/veryshyjelly/bifrost/internal/ingress"
)

func TestIngressCommitsBeforeAcknowledgementAndDeduplicatesAfterReopen(t *testing.T) {
	s := testStore(t)
	options := ingress.Options{AccountID: "fixture-account", SessionID: "current", AdminToken: strings.Repeat("a", 32), WebhookSecret: strings.Repeat("w", 32), OwnerID: 123, ManagementChatID: -100, MasterBotID: 999}
	input, err := ingress.New(options, s)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"sessionId":"current","event":"message.received","idempotencyKey":"shared-key","data":{"chatId":"fixture@g.us"}}`
	invoke := func(input *ingress.Service) bool {
		r := httptest.NewRequest("POST", "/webhooks/openwa", strings.NewReader(body))
		r.Header.Set("X-OpenWA-Signature", "sha256="+ingress.TelegramSecret(body, options.WebhookSecret))
		w := httptest.NewRecorder()
		httpserver.Handler(s, options.AdminToken, httpserver.Options{Ingress: input}).ServeHTTP(w, r)
		if w.Code != 200 {
			t.Errorf("status %d: %s", w.Code, w.Body)
		}
		var result struct{ Accepted bool }
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Error(err)
		}
		return result.Accepted
	}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Go(func() {
			if invoke(input) {
				accepted.Add(1)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("duplicate admissions %d", accepted.Load())
	}
	counts, err := s.Counts(t.Context())
	if err != nil || counts["pending"] != 1 {
		t.Fatal(counts, err)
	}
	reopened, err := Open(t.Context(), s.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	input, err = ingress.New(options, reopened)
	if err != nil {
		t.Fatal(err)
	}
	if invoke(input) {
		t.Fatal("duplicate after reopen")
	}
	job := claim(t, s)
	if job.Dedup != "ow:shared-key" || job.Stream != "wa:fixture@g.us" {
		t.Fatal(job.Dedup, job.Stream)
	}
}

func TestIngressObservationAndDeliveryCommitOrRollbackTogether(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	if _, err := s.pool.Exec(ctx, "ALTER TABLE jobs ADD CONSTRAINT fixture_ingress_reject CHECK (kind <> 'reject-fixture')"); err != nil {
		t.Fatal(err)
	}
	observation := &ingress.Observation{AccountID: "fixture-account", Scope: "*", Bot: "master", Kind: "service", ChatID: -100, MessageID: 7, Payload: json.RawMessage(`{"message_id":7}`)}
	event := &delivery.Event{AccountID: "fixture-account", Dedup: "tg:master:1", Stream: "tg:-100:0", Kind: "reject-fixture", Payload: json.RawMessage(`{"update_id":1}`)}
	if _, err := s.Admit(ctx, event, observation); err == nil {
		t.Fatal("constraint failure was accepted")
	}
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM telegram_observations").Scan(&n); err != nil || n != 0 {
		t.Fatal("partial observation commit", n, err)
	}
	event.Kind = "tg"
	ok, err := s.Admit(ctx, event, observation)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	ok, err = s.Admit(ctx, event, observation)
	if err != nil || ok {
		t.Fatal("duplicate", ok, err)
	}
	var seq int64
	if err := s.pool.QueryRow(ctx, "SELECT seq FROM telegram_observations").Scan(&seq); err != nil {
		t.Fatal(err)
	}
	observation.Kind = "chat"
	if _, err := s.Admit(ctx, nil, observation); err != nil {
		t.Fatal(err)
	}
	observation.Kind = "service"
	if _, err := s.Admit(ctx, nil, observation); err != nil {
		t.Fatal(err)
	}
	var after int64
	var kind string
	if err := s.pool.QueryRow(ctx, "SELECT seq,kind FROM telegram_observations").Scan(&after, &kind); err != nil || after != seq || kind != "chat" {
		t.Fatal("cleanup protection changed", after, kind, err)
	}
	counts, _ := s.Counts(ctx)
	if counts["pending"] != 1 {
		t.Fatal("observation-only admitted delivery", counts)
	}
}

func TestIngressDirectoryResolvesWholeGroupsAndSkipsRetiredInbox(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.PutIngressBot(ctx, "fixture-account", ingress.Bot{ID: "200", Username: "PeerBot"}); err != nil {
		t.Fatal(err)
	}
	if bot, ok, err := s.IngressBot(ctx, "fixture-account", "200"); err != nil || !ok || bot.Username != "PeerBot" {
		t.Fatal(bot, ok, err)
	}
	if _, ok, err := s.IngressBot(ctx, "different-account", "200"); err != nil || ok {
		t.Fatal("cross-account bot", ok, err)
	}
	for _, r := range []ingress.Route{{WhatsAppChat: "mirror@g.us", TelegramChatID: -200}, {WhatsAppChat: "retired@g.us", TelegramChatID: -100, InboxRetired: true}, {WhatsAppChat: "topic@g.us", TelegramChatID: -100, ThreadID: 44}} {
		if err := s.PutIngressRoute(ctx, "fixture-account", r); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		chat, thread, want int64
		found              bool
	}{{-200, 999, 0, true}, {-100, 0, 0, false}, {-100, 44, 44, true}, {-100, 45, 0, false}} {
		r, found, err := s.IngressRoute(ctx, "fixture-account", tc.chat, tc.thread, -100)
		if err != nil || found != tc.found || r.ThreadID != tc.want {
			t.Fatal(tc, r, found, err)
		}
	}
}
