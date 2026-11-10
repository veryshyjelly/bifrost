package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	tg "github.com/veryshyjelly/bifrost/internal/providers/telegram"
)

type inboxCommitFailure struct {
	*Store
	fail atomic.Bool
}

func (s *inboxCommitFailure) CreateInbox(ctx context.Context, j delivery.Job, r conversation.Route) (conversation.Route, error) {
	if s.fail.CompareAndSwap(true, false) {
		return conversation.Route{}, errors.New("fixture local persistence failure")
	}
	return s.Store.CreateInbox(ctx, j, r)
}

func TestInboxCreationReusesTopicReceiptAfterLocalFailure(t *testing.T) {
	s := testStore(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.HasSuffix(r.URL.Path, "createForumTopic") {
			t.Error("unexpected provider operation")
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		if r.FormValue("chat_id") != "-100" || r.FormValue("name") != "Original" {
			t.Error("topic request changed")
		}
		fmt.Fprint(w, `{"ok":true,"result":{"message_thread_id":44,"name":"Original"}}`)
	}))
	defer server.Close()
	client, err := tg.New(server.URL, "private-fixture-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := &inboxCommitFailure{Store: s}
	wrapper.fail.Store(true)
	routes, err := conversation.New(wrapper, client, -100, 123)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: "ow:first", Stream: "wa:peer@c.us", Kind: "wa", Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	h := func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		name := "Original"
		if j.Attempts > 1 {
			name = "Changed"
		}
		_, err := routes.EnsureInbox(ctx, j, e, "peer@c.us", name)
		return err
	}
	r := executeWorker(t, s, "wa", h)
	if r.State != delivery.Pending {
		t.Fatal(r)
	}
	if _, found, err := s.ConversationRoute(t.Context(), "fixture-account", "peer@c.us"); err != nil || found {
		t.Fatal("partial route commit", found, err)
	}
	if _, err := s.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	r = executeWorker(t, s, "wa", h)
	if r.State != delivery.Done || calls.Load() != 1 {
		t.Fatal(r, calls.Load())
	}
	route, found, err := s.ConversationRoute(t.Context(), "fixture-account", "peer@c.us")
	if err != nil || !found || route.ThreadID != 44 || route.Mode != conversation.Inbox {
		t.Fatal(route, found, err)
	}
	var n int
	if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE dedup='management:new:peer@c.us'").Scan(&n); err != nil || n != 1 {
		t.Fatal("management notification not atomic/deduplicated", n, err)
	}
}

func TestBoundBotCannotMoveToAnotherPersonAndStartQueuesIndependentRetirement(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	if err := s.PutIngressBot(ctx, "fixture-account", ingress.Bot{ID: "200", Username: "PeerBot"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(ctx, delivery.Event{AccountID: "fixture-account", Dedup: "tg:master:1", Kind: "tg", Stream: "tg:master:123", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	job := claim(t, s)
	if err := s.BindContactBot(ctx, job, "peer@c.us", "200"); err != nil {
		t.Fatal(err)
	}
	if err := s.BindContactBot(ctx, job, "different@c.us", "200"); !errors.Is(err, delivery.ErrInvalid) {
		t.Fatal("bot reassigned to another contact", err)
	}
	if err := s.StartContactBot(ctx, job, "200"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetJobState(ctx, job, delivery.Done, 0); err != nil {
		t.Fatal(err)
	}
	binding, found, err := s.ContactBot(ctx, "fixture-account", "peer@c.us")
	if err != nil || !found || !binding.Started || binding.BotID != "200" {
		t.Fatal(binding, found, err)
	}
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE kind='control-retire-inbox' AND stream='retire-inbox:peer@c.us'").Scan(&n); err != nil || n != 1 {
		t.Fatal("retirement not queued independently", n, err)
	}
}

func TestStartedBotCreatesDirectRouteWithoutTopicAndKeepsCleanupSeparate(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	if err := s.PutIngressBot(ctx, "fixture-account", ingress.Bot{ID: "200"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(ctx, delivery.Event{AccountID: "fixture-account", Dedup: "binding", Kind: "tg", Stream: "tg:master", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	j := claim(t, s)
	if err := s.BindContactBot(ctx, j, "peer@c.us", "200"); err != nil {
		t.Fatal(err)
	}
	if err := s.StartContactBot(ctx, j, "200"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetJobState(ctx, j, delivery.Done, 0); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	client, _ := tg.New(server.URL, "private-fixture-token", nil)
	routes, _ := conversation.New(s, client, -100, 123)
	if _, err := s.Enqueue(ctx, delivery.Event{AccountID: "fixture-account", Dedup: "ow:new", Kind: "wa", Stream: "wa:peer@c.us", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	r := executeWorker(t, s, "wa", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		_, err := routes.EnsureInbox(ctx, j, e, "peer@c.us", "Peer")
		return err
	})
	if r.State != delivery.Done || calls.Load() != 0 {
		t.Fatal(r, calls.Load())
	}
	route, found, err := s.ConversationRoute(ctx, "fixture-account", "peer@c.us")
	if err != nil || !found || route.Mode != conversation.Dedicated || !route.InboxRetired || route.ThreadID != 0 {
		t.Fatal(route, found, err)
	}
	// The WA conversation did not overtake or consume the separate cleanup job.
	var state string
	if err := s.pool.QueryRow(ctx, "SELECT state FROM jobs WHERE kind='control-retire-inbox'").Scan(&state); err != nil || state != "pending" {
		t.Fatal(state, err)
	}
}
