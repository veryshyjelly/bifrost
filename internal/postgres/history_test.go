package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/history"
	wa "github.com/veryshyjelly/bifrost/internal/providers/openwa"
)

type notYetHistoryDelivery struct{}

func (notYetHistoryDelivery) DeliverHistory(context.Context, delivery.Job, *delivery.Effects, history.Run, history.Item) error {
	return delivery.ErrUnsupported
}

type historyFixture struct {
	f        *relayFixture
	service  *history.Service
	reader   *wa.Client
	reads    atomic.Int32
	failure  atomic.Bool
	batch    atomic.Value
	settings history.Settings
}

func newHistoryFixture(t *testing.T) *historyFixture {
	t.Helper()
	h := &historyFixture{f: newRelayFixture(t, nil, true), settings: history.Settings{Session: "fixture-session", Timezone: "Asia/Kolkata", OwnerID: 123, Management: -100, DefaultCount: 20, CommandTTL: 60, ControlTTL: 600}}
	h.batch.Store([]sdk.ChatHistoryMessage{{ID: "child", ChatID: "peer@g.us", Author: "12345000000001@lid", Body: "Child", Timestamp: 1, Type: sdk.MsgText, QuotedMessage: &sdk.QuotedMessage{ID: "parent", Body: "Parent"}}, {ID: "parent", ChatID: "peer@g.us", Author: "12345000000001@lid", Body: "Parent", Timestamp: 2, Type: sdk.MsgText}, {ID: "revoked", ChatID: "peer@g.us", Type: sdk.MessageType("revoked"), Timestamp: 3}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.reads.Add(1)
		if h.failure.Load() {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"message":"fixture unavailable"}`)
			return
		}
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/history") || r.URL.Query().Get("includeMedia") != "false" {
			t.Error("wrong history request")
		}
		if r.URL.Query().Get("limit") == "150" && r.URL.Query().Get("deep") != "true" {
			t.Error("deep history not requested")
		}
		json.NewEncoder(w).Encode(h.batch.Load())
	}))
	t.Cleanup(server.Close)
	client, err := wa.New(server.URL, "fixture-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	h.reader = client
	replies, err := control.New(h.f.store, h.f.replyClients, -100)
	if err != nil {
		t.Fatal(err)
	}
	h.service, err = history.New(h.f.store, client, notYetHistoryDelivery{}, replies, h.settings)
	if err != nil {
		t.Fatal(err)
	}
	h.f.route(t, "peer@g.us", -100, 44)
	return h
}
func (h *historyFixture) update(t *testing.T, u models.Update) delivery.Report {
	t.Helper()
	if !admitPicker(t, h.f, "master", u) {
		t.Fatal("history update not admitted")
	}
	var id int64
	if err := h.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", fmt.Sprintf("tg:master:%d", u.ID)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return h.f.run(t, id, "tg", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		return h.service.Handle(ctx, j, e, "master", &u)
	})
}
func (h *historyFixture) prompt(t *testing.T, text string) (string, int) {
	t.Helper()
	if r := h.update(t, models.Update{ID: 100, Message: pickerMessage(101, -100, 44, text)}); r.State != delivery.Done {
		t.Fatal(r)
	}
	var id, plan string
	if err := h.f.store.pool.QueryRow(t.Context(), "SELECT choice_id,payload->>'PlanKey' FROM history_choices").Scan(&id, &plan); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := h.f.store.pool.QueryRow(t.Context(), "SELECT result FROM effects WHERE effect_key=$1", plan+":text:0").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var m models.Message
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return id, m.ID
}
func historyCallback(id int64, choice, mode string, message int, thread int64) models.Update {
	return models.Update{ID: id, CallbackQuery: &models.CallbackQuery{ID: fmt.Sprint(id), From: models.User{ID: 123}, Data: "history:" + mode + ":" + choice, Message: models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeMessage, Message: pickerMessage(message, -100, thread, "")}}}
}
func TestHistoryPromptStartsNoFetchOrLiveHoldAndCancelStaysPassive(t *testing.T) {
	h := newHistoryFixture(t)
	choice, message := h.prompt(t, "/history 150")
	var count int
	if err := h.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM history_runs").Scan(&count); err != nil || count != 0 || h.reads.Load() != 0 {
		t.Fatal(count, err)
	}
	live := h.f.enqueue(t, "wa", "before-choice", "wa:peer@g.us", "")
	j := claim(t, h.f.store)
	if j.ID != live {
		t.Fatal("choice prompt held live work", j)
	}
	finish(t, h.f.store, j)
	if r := h.update(t, historyCallback(101, choice, "cancel", message, 44)); r.State != delivery.Done {
		t.Fatal(r)
	}
	if err := h.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM history_runs").Scan(&count); err != nil || count != 0 || h.reads.Load() != 0 {
		t.Fatal("cancel fetched history", count, err)
	}
	var cleanup int
	if err := h.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE dedup=$1", "history-choice-delete:"+choice).Scan(&cleanup); err != nil || cleanup != 1 {
		t.Fatal(cleanup, err)
	}
}
func TestHistoryChoiceIsScopedSingleUseAndHoldsOnlyItsConversation(t *testing.T) {
	h := newHistoryFixture(t)
	choice, message := h.prompt(t, "/history 150")
	if r := h.update(t, historyCallback(101, choice, "all", message, 45)); r.State != delivery.Done {
		t.Fatal(r)
	}
	var count int
	h.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM history_runs").Scan(&count)
	if count != 0 {
		t.Fatal("wrong topic accepted")
	}
	if r := h.update(t, historyCallback(102, choice, "all", message, 44)); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := h.update(t, historyCallback(103, choice, "skip", message, 44)); r.State != delivery.Done {
		t.Fatal(r)
	}
	var mode string
	if err := h.f.store.pool.QueryRow(t.Context(), "SELECT count(*),min(payload->>'Mode') FROM history_runs").Scan(&count, &mode); err != nil || count != 1 || mode != "all" {
		t.Fatal(count, mode, err)
	}
	if h.reads.Load() != 0 {
		t.Fatal("choice fetched inline")
	}
	if _, err := h.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp()+interval '1 hour' WHERE state='pending' AND kind<>'wa'"); err != nil {
		t.Fatal(err)
	}
	h.f.enqueue(t, "wa", "held-live", "wa:peer@g.us", "")
	other := h.f.enqueue(t, "wa", "other-live", "wa:other@g.us", "")
	j := claim(t, h.f.store)
	if j.ID != other {
		t.Fatal("history held unrelated conversation", j)
	}
	finish(t, h.f.store, j)
	if _, err := h.f.store.Claim(t.Context(), time.Minute); err != delivery.ErrNoJob {
		t.Fatal("history live hold bypassed", err)
	}
}
func TestHistoryFetchFreezesCompleteOrderedBatchAndReusesIt(t *testing.T) {
	h := newHistoryFixture(t)
	choice, message := h.prompt(t, "/history 150")
	if r := h.update(t, historyCallback(101, choice, "all", message, 44)); r.State != delivery.Done {
		t.Fatal(r)
	}
	// The independent prompt cleanup is tested by the cleanup capability.
	h.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp()+interval '1 hour' WHERE kind='control-expire' AND state='pending'")
	var fetch int64
	if err := h.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='history'").Scan(&fetch); err != nil {
		t.Fatal(err)
	}
	if r := h.f.run(t, fetch, "history", h.service.Fetch); r.State != delivery.Done {
		t.Fatal(r)
	}
	rows, err := h.f.store.pool.Query(t.Context(), "SELECT source_id FROM history_items ORDER BY item_index")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	if strings.Join(ids, ",") != "parent,child" {
		t.Fatal(ids)
	}
	if _, err := h.f.store.pool.Exec(t.Context(), "UPDATE jobs SET state='pending',due=clock_timestamp() WHERE id=$1", fetch); err != nil {
		t.Fatal(err)
	}
	if r := h.f.run(t, fetch, "history", h.service.Fetch); r.State != delivery.Done {
		t.Fatal(r)
	}
	if h.reads.Load() != 1 {
		t.Fatal("frozen batch refetched", h.reads.Load())
	}
}
func TestHistoryFetchFailureReleasesLiveHoldWithoutDeletingTopic(t *testing.T) {
	h := newHistoryFixture(t)
	h.failure.Store(true)
	choice, message := h.prompt(t, "/history")
	if r := h.update(t, historyCallback(101, choice, "skip", message, 44)); r.State != delivery.Done {
		t.Fatal(r)
	}
	h.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp()+interval '1 hour' WHERE kind='control-expire' AND state='pending'")
	var fetch int64
	h.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='history'").Scan(&fetch)
	if r := h.f.run(t, fetch, "history", h.service.Fetch); r.State != delivery.Failed {
		t.Fatal(r)
	}
	var state string
	if err := h.f.store.pool.QueryRow(t.Context(), "SELECT state FROM history_runs").Scan(&state); err != nil || state != "failed" {
		t.Fatal(state, err)
	}
	live := h.f.enqueue(t, "wa", "released-live", "wa:peer@g.us", "")
	j := claim(t, h.f.store)
	if j.ID != live {
		t.Fatal("failed history kept hold", j)
	}
	finish(t, h.f.store, j)
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	if len(h.f.deleteCalls) != 0 {
		t.Fatal("history failure deleted topic")
	}
}
