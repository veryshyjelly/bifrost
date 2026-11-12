package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/bridge"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/statuses"
)

type statusFixture struct {
	n        *nativeFixture
	s        *statuses.Service
	c        statuses.Settings
	controls *statuses.Controls
}

func TestStatusArchiveTopicCannotBecomeAnImportedConversationRoute(t *testing.T) {
	s := newStatusFixture(t)
	if r := s.post(t, "reserved-status", "first", "text", "Original", time.Now().Add(time.Hour)); r.State != delivery.Done {
		t.Fatal(r)
	}
	err := s.n.f.store.PutIngressRoute(t.Context(), "fixture-account", ingress.Route{WhatsAppChat: "peer@g.us", TelegramChatID: -100, ThreadID: 99})
	if !errors.Is(err, delivery.ErrInvalid) {
		t.Fatal("archive topic became a conversation", err)
	}
}

func TestStatusArchivePreparedDeliveryKeepsManagementTargetAfterConfigChange(t *testing.T) {
	s := newStatusFixture(t)
	s.n.failMethod, s.n.failCode = "sendMessage", 429
	first := s.post(t, "frozen-management", "story", "text", "Original", time.Now().Add(time.Hour))
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	c := s.c
	c.Management = -200
	service, err := statuses.New(s.n.f.store, s.n.waClient, s.n.tgClients["master"], s.n.native, c)
	if err != nil {
		t.Fatal(err)
	}
	s.n.mu.Lock()
	s.n.failMethod = ""
	s.n.mu.Unlock()
	if _, err := s.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", first.JobID); err != nil {
		t.Fatal(err)
	}
	if r := s.n.f.run(t, first.JobID, "wa", service.Receive); r.State != delivery.Done {
		t.Fatal(r)
	}
	s.n.mu.Lock()
	defer s.n.mu.Unlock()
	if len(s.n.calls) != 3 || s.n.calls[2].Chat != -100 || s.n.calls[2].Thread != 99 {
		t.Fatal("frozen archive target moved", s.n.calls)
	}
	var feeds int
	if err := s.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM status_feeds").Scan(&feeds); err != nil || feeds != 1 {
		t.Fatal(feeds, err)
	}
}

func TestStatusArchivePendingSetupCannotChangeFrozenManagement(t *testing.T) {
	s := newStatusFixture(t)
	s.n.dropMethod = "createForumTopic"
	first := s.post(t, "pending-management", "story", "text", "Original", time.Now().Add(time.Hour))
	if first.State != delivery.Uncertain {
		t.Fatal(first)
	}
	if err := s.n.f.store.Review(t.Context(), first.JobID, true, true); err != nil {
		t.Fatal(err)
	}
	c := s.c
	c.Management = -200
	if r := s.n.f.run(t, first.JobID, "wa", func(ctx context.Context, j delivery.Job, _ *delivery.Effects) error {
		_, err := s.n.f.store.PrepareStatusTopic(ctx, j, c)
		return err
	}); r.State != delivery.Failed {
		t.Fatal(r)
	}
	var feeds int
	if err := s.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM status_feeds").Scan(&feeds); err != nil || feeds != 1 {
		t.Fatal("competing management setup admitted", feeds, err)
	}
}

func TestStatusArchiveUnknownTopicSetupNeverCreatesCompetingTopic(t *testing.T) {
	s := newStatusFixture(t)
	s.n.dropMethod = "createForumTopic"
	if r := s.post(t, "unknown-topic", "story", "text", "Original", time.Now().Add(time.Hour)); r.State != delivery.Uncertain {
		t.Fatal(r)
	}
	s.n.mu.Lock()
	s.n.dropMethod = ""
	s.n.mu.Unlock()
	if r := s.refresh(t, "blocked-refresh"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	s.n.mu.Lock()
	defer s.n.mu.Unlock()
	if len(s.n.calls) != 1 || s.n.statusLists != 0 {
		t.Fatal(s.n.calls, s.n.statusLists)
	}
	var pending int
	if err := s.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM status_feeds WHERE NOT complete").Scan(&pending); err != nil || pending != 1 {
		t.Fatal(pending, err)
	}
}

func TestStatusArchiveOmittedMediaKeepsCaptionWithoutFetchingBytes(t *testing.T) {
	s := newStatusFixture(t)
	raw, _ := json.Marshal(map[string]any{"event": sdk.EventStatusReceived, "sessionId": s.c.Session, "idempotencyKey": "omitted", "data": map[string]any{"statusId": "omitted", "contact": sdk.StatusContact{ID: "12345000000001@lid", Name: "Alice"}, "type": "image", "caption": "Retain caption", "postedAt": time.Now().UnixMilli(), "expiresAt": time.Now().Add(time.Hour).UnixMilli(), "hasMedia": true, "mediaOmitted": true}})
	if !fixtureIngress(t, s.n.f)("openwa", raw) {
		t.Fatal("not admitted")
	}
	if r := s.n.f.dispatch(t, "ow:omitted", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	s.n.mu.Lock()
	defer s.n.mu.Unlock()
	if s.n.reads != 0 || len(s.n.calls) != 2 || s.n.calls[1].Method != "sendMessage" || !strings.Contains(s.n.calls[1].Text, "Retain caption") || !strings.Contains(s.n.calls[1].Text, "unavailable") {
		t.Fatal(s.n.reads, s.n.calls)
	}
}

func TestStatusArchiveStartupRequiresConfirmedLocalMigration(t *testing.T) {
	s := newStatusFixture(t)
	if queued, err := s.n.f.store.QueueStatusStartup(t.Context(), "fixture-account", "startup", s.c); err != nil || queued {
		t.Fatal(queued, err)
	}
	if err := s.n.f.store.queries.SaveBotMigration(t.Context(), db.SaveBotMigrationParams{AccountID: "fixture-account", BotID: "master", Identity: []byte(`{"id":999,"is_bot":true}`), MigratedAt: time.Now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if queued, err := s.n.f.store.QueueStatusStartup(t.Context(), "fixture-account", "startup", s.c); err != nil || !queued {
		t.Fatal(queued, err)
	}
	var id int64
	if err := s.n.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup='startup'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if r := s.n.f.run(t, id, "status-startup", s.s.Refresh); r.State != delivery.Done {
		t.Fatal(r)
	}
	s.n.mu.Lock()
	defer s.n.mu.Unlock()
	if s.n.statusLists != 1 || len(s.n.calls) != 1 || len(s.n.sends) != 0 {
		t.Fatal(s.n.statusLists, s.n.calls)
	}
}

type statusRefreshCrash struct {
	*Store
	fail bool
}

func (s *statusRefreshCrash) EnqueueStatusBatch(ctx context.Context, j delivery.Job, c statuses.Settings) error {
	if s.fail {
		s.fail = false
		return delivery.ErrLocalRetry
	}
	return s.Store.EnqueueStatusBatch(ctx, j, c)
}

func TestStatusArchiveFrozenRefreshBatchSurvivesCrashWithoutRefetch(t *testing.T) {
	s := newStatusFixture(t)
	store := &statusRefreshCrash{Store: s.n.f.store, fail: true}
	service, err := statuses.New(store, s.n.waClient, s.n.tgClients["master"], s.n.native, s.c)
	if err != nil {
		t.Fatal(err)
	}
	s.s = service
	s.n.statusRecords = []sdk.StatusRecord{{ID: "frozen-story", Contact: sdk.StatusContact{ID: "12345000000001@lid", Name: "Alice"}, Type: "text", Caption: "Original frozen body", Timestamp: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}}
	first := s.refresh(t, "refresh-crash")
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	s.n.mu.Lock()
	s.n.statusRecords[0].Caption = "Changed provider body"
	s.n.mu.Unlock()
	if _, err := s.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", first.JobID); err != nil {
		t.Fatal(err)
	}
	if r := s.n.f.run(t, first.JobID, "status-refresh", service.Refresh); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := s.n.f.dispatch(t, "ow:status-post:-100:frozen-story", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	s.n.mu.Lock()
	defer s.n.mu.Unlock()
	if s.n.statusLists != 1 || len(s.n.calls) != 2 || !strings.Contains(s.n.calls[1].Text, "Original frozen body") {
		t.Fatal(s.n.statusLists, s.n.calls)
	}
}

func newStatusFixture(t *testing.T) *statusFixture {
	t.Helper()
	n := newNativeFixture(t, nil)
	c := statuses.Settings{Session: "fixture-session", OwnerID: 123, Management: -100, CommandTTL: 60, NoticeTTL: 60, ControlTTL: 600}
	s, err := statuses.New(n.f.store, n.waClient, n.tgClients["master"], n.native, c)
	if err != nil {
		t.Fatal(err)
	}
	replies, err := control.New(n.f.store, func(_ context.Context, _ string, id string) (control.Telegram, error) { return n.tgClients[id], nil }, -100)
	if err != nil {
		t.Fatal(err)
	}
	controls, err := statuses.NewControls(n.f.store, replies, func(_ context.Context, _ string, id string) (statuses.ControlTelegram, error) {
		return n.tgClients[id], nil
	}, s, c)
	if err != nil {
		t.Fatal(err)
	}
	options := n.f.dispatchOptions
	options.Extensions.PassiveTopics = controls.Handle
	if options.Extensions.ControlJobs == nil {
		options.Extensions.ControlJobs = map[string]delivery.Handler{}
	}
	for kind, h := range controls.Handlers() {
		options.Extensions.ControlJobs[kind] = h
	}
	options.Extensions.WhatsApp = map[sdk.WebhookEvent]delivery.Handler{sdk.EventStatusReceived: s.Receive, sdk.EventMessageReceived: n.native.Incoming}
	n.f.dispatcher, err = bridge.New(n.f.store, n.f.service, n.f.whatsapp, options)
	if err != nil {
		t.Fatal(err)
	}
	return &statusFixture{n: n, s: s, c: c, controls: controls}
}
func (s *statusFixture) post(t *testing.T, key, id, kind, caption string, expires time.Time) delivery.Report {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"event": sdk.EventStatusReceived, "sessionId": s.c.Session, "idempotencyKey": key, "data": map[string]any{"statusId": id, "contact": sdk.StatusContact{ID: "12345000000001@lid", Name: "Alice <b>"}, "type": kind, "caption": caption, "postedAt": time.Now().Add(-time.Minute).UnixMilli(), "expiresAt": expires.UnixMilli(), "hasMedia": kind != "text", "mediaUrl": "https://untrusted.invalid/tracker"}})
	if !fixtureIngress(t, s.n.f)("openwa", raw) {
		t.Fatal("post not admitted")
	}
	return s.n.f.dispatch(t, "ow:"+key, "wa")
}
func (s *statusFixture) refresh(t *testing.T, key string) delivery.Report {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"Session": s.c.Session, "Management": s.c.Management})
	if _, err := s.n.f.store.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: key, Kind: "status-refresh", Stream: "status-control:-100", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := s.n.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return s.n.f.run(t, id, "status-refresh", s.s.Refresh)
}

func TestStatusArchiveNativeMediaIsPassiveAndNeverCreatesConversation(t *testing.T) {
	s := newStatusFixture(t)
	if r := s.post(t, "status-webhook", "story", "image", "Original caption", time.Now().Add(time.Hour)); r.State != delivery.Done {
		t.Fatal(r)
	}
	s.n.mu.Lock()
	defer s.n.mu.Unlock()
	if len(s.n.calls) != 2 || s.n.calls[0].Method != "createForumTopic" || s.n.calls[1].Method != "sendPhoto" || !strings.Contains(s.n.calls[1].Text, "Original caption") || !strings.Contains(s.n.calls[1].Text, "Status") || !strings.Contains(s.n.calls[1].Text, "Alice <b>") || s.n.reads != 1 || len(s.n.sends) != 0 {
		t.Fatal(s.n.calls, s.n.reads, s.n.sends)
	}
	var routes, notices, done int
	if err := s.n.f.store.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM bridge_routes),(SELECT count(*) FROM jobs WHERE kind='management'),(SELECT count(*) FROM status_posts WHERE complete)").Scan(&routes, &notices, &done); err != nil || routes != 0 || notices != 0 || done != 1 {
		t.Fatal(routes, notices, done, err)
	}
	if s.n.f.reads.Load() != 0 {
		t.Fatal("Status was marked read")
	}
}

func TestStatusArchiveRefreshWebhookRaceSendsOneCopy(t *testing.T) {
	s := newStatusFixture(t)
	now := time.Now()
	s.n.statusRecords = []sdk.StatusRecord{{ID: "race", Contact: sdk.StatusContact{ID: "12345000000001@lid", Name: "Alice"}, Type: "text", Caption: "Original", Timestamp: now, ExpiresAt: now.Add(time.Hour)}}
	if r := s.refresh(t, "refresh-race"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := s.post(t, "webhook-race", "race", "text", "Original", now.Add(time.Hour)); r.State != delivery.Done {
		t.Fatal(r)
	}
	var copies, posts, feeds int
	if err := s.n.f.store.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM message_mappings),(SELECT count(*) FROM status_posts),(SELECT count(*) FROM status_feeds WHERE complete)").Scan(&copies, &posts, &feeds); err != nil || copies != 1 || posts != 1 || feeds != 1 {
		t.Fatal(copies, posts, feeds, err)
	}
	s.n.mu.Lock()
	defer s.n.mu.Unlock()
	if s.n.statusLists != 1 || len(s.n.calls) != 2 || len(s.n.sends) != 0 {
		t.Fatal(s.n.statusLists, s.n.calls)
	}
}

func TestStatusArchiveExpiryLeavesExistingCopiesAndRejectsNewExpiredPosts(t *testing.T) {
	s := newStatusFixture(t)
	if r := s.post(t, "alive", "story", "text", "Keep archive", time.Now().Add(time.Hour)); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := s.post(t, "expired-repeat", "story", "text", "Expired body", time.Now().Add(-time.Second)); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := s.post(t, "expired-new", "old-story", "text", "Old", time.Now().Add(-time.Second)); r.State != delivery.Done {
		t.Fatal(r)
	}
	s.n.mu.Lock()
	defer s.n.mu.Unlock()
	if len(s.n.calls) != 2 {
		t.Fatal(s.n.calls)
	}
	var copies int
	if err := s.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_mappings WHERE NOT deleted").Scan(&copies); err != nil || copies != 1 {
		t.Fatal(copies, err)
	}
}

func TestStatusArchiveUnknownSendIsNeverAdoptedByRefresh(t *testing.T) {
	s := newStatusFixture(t)
	s.n.dropMethod = "sendMessage"
	if r := s.post(t, "unknown-status", "story", "text", "Original", time.Now().Add(time.Hour)); r.State != delivery.Uncertain {
		t.Fatal(r)
	}
	s.n.mu.Lock()
	s.n.dropMethod = ""
	s.n.statusRecords = []sdk.StatusRecord{{ID: "story", Contact: sdk.StatusContact{ID: "12345000000001@lid"}, Type: "text", Caption: "Original", Timestamp: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}}
	s.n.mu.Unlock()
	if r := s.refresh(t, "refresh-unknown"); r.State != delivery.Done {
		t.Fatal(r)
	}
	s.n.mu.Lock()
	defer s.n.mu.Unlock()
	if len(s.n.calls) != 2 {
		t.Fatal(s.n.calls)
	}
	var uncertain int
	if err := s.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM effects WHERE state='uncertain'").Scan(&uncertain); err != nil || uncertain != 1 {
		t.Fatal(uncertain, err)
	}
}
