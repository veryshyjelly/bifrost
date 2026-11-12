package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/bridge"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/polls"
)

type pollFixture struct {
	f *changeFixture
	s *polls.Service
}

func newPollFixture(t *testing.T) *pollFixture {
	t.Helper()
	f := newChangeFixture(t)
	s, err := polls.New(f.n.f.store, f.n.waClient, f.replies, polls.Settings{Session: "fixture-session", OwnerID: 123, NoticeTTL: 90})
	if err != nil {
		t.Fatal(err)
	}
	options := f.n.f.dispatchOptions
	options.Extensions.WhatsApp = map[sdk.WebhookEvent]delivery.Handler{sdk.EventMessageReceived: f.n.native.Incoming, sdk.EventMessageSent: f.n.native.Incoming, sdk.EventMessageEdited: f.edits.WhatsApp, sdk.EventPollVote: s.Observe}
	options.Extensions.Telegram = func(ctx context.Context, j delivery.Job, e *delivery.Effects, id string, u *models.Update) error {
		for _, h := range []bridge.TelegramHandler{s.Callback, f.edits.Telegram, f.n.outbound.Handle} {
			if err := h(ctx, j, e, id, u); !errors.Is(err, delivery.ErrUnsupported) {
				return err
			}
		}
		return delivery.ErrUnsupported
	}
	if options.Extensions.ControlJobs == nil {
		options.Extensions.ControlJobs = map[string]delivery.Handler{}
	}
	options.Extensions.ControlJobs["control-notice"] = f.replies.Notice
	options.Extensions.ControlJobs["control-poll-vote"] = s.Vote
	f.n.f.dispatcher, err = bridge.New(f.n.f.store, f.n.f.service, f.n.f.whatsapp, options)
	if err != nil {
		t.Fatal(err)
	}
	return &pollFixture{f: f, s: s}
}
func (f *pollFixture) source(t *testing.T, multiple bool) map[string]string {
	t.Helper()
	message := sdk.ChatHistoryMessage{ID: "source-poll", ChatID: "peer@g.us", Author: "12345000000001@lid", IsGroup: true, Type: sdk.MsgPoll, Poll: &sdk.ChatHistoryPoll{Name: "Which?", Options: []string{" A ", "B"}, AllowMultipleAnswers: multiple}, Timestamp: 1700000000}
	raw, _ := json.Marshal(map[string]any{"event": "message.received", "sessionId": "fixture-session", "idempotencyKey": "source-poll", "data": message})
	if !fixtureIngress(t, f.f.n.f)("openwa", raw) {
		t.Fatal("poll not admitted")
	}
	if r := f.f.n.f.dispatch(t, "ow:source-poll", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	rows, err := f.f.n.f.store.pool.Query(t.Context(), "SELECT action_id,payload->>'Choice' FROM native_poll_actions")
	if err != nil {
		t.Fatal(err)
	}
	choices := map[string]string{}
	for rows.Next() {
		var id, choice string
		if err := rows.Scan(&id, &choice); err != nil {
			t.Fatal(err)
		}
		choices[choice] = id
	}
	rows.Close()
	if len(choices) != 2 {
		t.Fatal(choices)
	}
	return choices
}
func (f *pollFixture) callback(t *testing.T, update int64, choice string, thread int64, message int) delivery.Report {
	t.Helper()
	m := pickerMessage(message, -100, thread, "")
	u := models.Update{ID: update, CallbackQuery: &models.CallbackQuery{ID: fmt.Sprint(update), From: models.User{ID: 123}, Data: "vote:" + choice, Message: models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeMessage, Message: m}}}
	if !admitPicker(t, f.f.n.f, "master", u) {
		t.Fatal("callback not admitted")
	}
	return f.f.n.f.dispatch(t, "tg:master:"+strconv.FormatInt(update, 10), "tg")
}
func (f *pollFixture) pending(t *testing.T) []int64 {
	t.Helper()
	rows, err := f.f.n.f.store.pool.Query(t.Context(), "SELECT id FROM jobs WHERE kind='control-poll-vote' AND state='pending' ORDER BY id")
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
func (f *pollFixture) run(t *testing.T, id int64) delivery.Report {
	t.Helper()
	return f.f.n.f.run(t, id, "control-poll-vote", f.s.Vote)
}

func TestPollChoicesSerializeTogglesAndFreezeExactNativeOptionTexts(t *testing.T) {
	f := newPollFixture(t)
	choices := f.source(t, true)
	if r := f.callback(t, 101, choices[" A "], 44, 2001); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.callback(t, 102, choices["B"], 44, 2001); r.State != delivery.Done {
		t.Fatal(r)
	}
	ids := f.pending(t)
	if len(ids) != 2 {
		t.Fatal(ids)
	}
	for _, id := range ids {
		if r := f.run(t, id); r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	for i, choice := range []string{" A ", "B"} {
		if r := f.callback(t, int64(103+i), choices[choice], 44, 2001); r.State != delivery.Done {
			t.Fatal(r)
		}
		ids := f.pending(t)
		if len(ids) != 1 {
			t.Fatal(ids)
		}
		if r := f.run(t, ids[0]); r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	if len(f.f.n.sends) != 4 {
		t.Fatal(f.f.n.sends)
	}
	expected := []string{`[" A "]`, `[" A ","B"]`, `["B"]`, `[]`}
	for i, call := range f.f.n.sends {
		if call.Method != "vote-poll" || string(call.Request["pollMessageId"]) != `"source-poll"` || string(call.Request["options"]) != expected[i] {
			t.Fatal(i, call)
		}
	}
}
func TestPollChoiceWrongScopeExpiredAndDuplicateStayPassive(t *testing.T) {
	f := newPollFixture(t)
	choices := f.source(t, false)
	for i, target := range []struct {
		thread  int64
		message int
	}{{45, 2001}, {44, 2002}} {
		if r := f.callback(t, int64(101+i), choices[" A "], target.thread, target.message); r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	if ids := f.pending(t); len(ids) != 0 {
		t.Fatal("wrong scope queued vote", ids)
	}
	if r := f.callback(t, 103, choices[" A "], 44, 2001); r.State != delivery.Done {
		t.Fatal(r)
	}
	ids := f.pending(t)
	if len(ids) != 1 {
		t.Fatal(ids)
	}
	if r := f.run(t, ids[0]); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET state='pending',due=clock_timestamp() WHERE dedup='tg:master:103'")
	if r := f.f.n.f.dispatch(t, "tg:master:103", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if ids := f.pending(t); len(ids) != 0 {
		t.Fatal("duplicate callback queued second vote", ids)
	}
	f.f.n.f.store.pool.Exec(t.Context(), "UPDATE native_poll_actions SET payload=jsonb_set(payload,'{ExpiresAt}','1')")
	if r := f.callback(t, 104, choices["B"], 44, 2001); r.State != delivery.Done {
		t.Fatal(r)
	}
	if ids := f.pending(t); len(ids) != 0 {
		t.Fatal("expired choice queued vote", ids)
	}
}
func TestPollSingleChoiceReplacesAndRefusedVotePreservesPriorSelection(t *testing.T) {
	f := newPollFixture(t)
	choices := f.source(t, false)
	if r := f.callback(t, 101, choices[" A "], 44, 2001); r.State != delivery.Done {
		t.Fatal(r)
	}
	ids := f.pending(t)
	if r := f.run(t, ids[0]); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.sendFailure = 501
	f.f.n.sendFailMethod = "vote-poll"
	if r := f.callback(t, 102, choices["B"], 44, 2001); r.State != delivery.Done {
		t.Fatal(r)
	}
	ids = f.pending(t)
	if r := f.run(t, ids[0]); r.State != delivery.Done {
		t.Fatal(r)
	}
	var raw []byte
	if err := f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT options FROM poll_selections").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var options []string
	json.Unmarshal(raw, &options)
	if len(options) != 1 || options[0] != " A " {
		t.Fatal("refusal changed selection", options)
	}
	var ttl int
	if err := f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT (payload->>'TTLSeconds')::int FROM jobs WHERE kind='control-notice'").Scan(&ttl); err != nil || ttl != 90 {
		t.Fatal(ttl, err)
	}
	f.f.n.mu.Lock()
	f.f.n.sendFailure = 0
	f.f.n.mu.Unlock()
	if r := f.callback(t, 103, choices["B"], 44, 2001); r.State != delivery.Done {
		t.Fatal(r)
	}
	ids = f.pending(t)
	if r := f.run(t, ids[0]); r.State != delivery.Done {
		t.Fatal(r)
	}
	if err := f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT options FROM poll_selections").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(raw, &options)
	if len(options) != 1 || options[0] != "B" {
		t.Fatal(options)
	}
}
func TestPollUnknownTransportRetriesFrozenSelectionWithoutReview(t *testing.T) {
	f := newPollFixture(t)
	choices := f.source(t, true)
	if r := f.callback(t, 101, choices[" A "], 44, 2001); r.State != delivery.Done {
		t.Fatal(r)
	}
	ids := f.pending(t)
	f.f.n.dropSend = true
	first := f.run(t, ids[0])
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	f.f.n.mu.Lock()
	f.f.n.dropSend = false
	f.f.n.mu.Unlock()
	f.f.n.f.store.pool.Exec(t.Context(), "UPDATE poll_selections SET options='[\"B\"]'")
	f.f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", ids[0])
	if r := f.run(t, ids[0]); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	if len(f.f.n.sends) != 2 || string(f.f.n.sends[0].Request["options"]) != string(f.f.n.sends[1].Request["options"]) {
		t.Fatal("vote retry changed frozen selection", f.f.n.sends)
	}
}

func TestPollNativeVoteObservationQueuesRetainedScopedNoticeWithoutRead(t *testing.T) {
	f := newPollFixture(t)
	f.source(t, true)
	raw, _ := json.Marshal(map[string]any{"event": "poll.vote", "sessionId": "fixture-session", "idempotencyKey": "poll-vote-observed", "data": map[string]any{"chatId": "peer@g.us", "pollMessageId": "source-poll", "voterId": "12345000000001@lid", "selectedOptions": []string{"B"}}})
	if !fixtureIngress(t, f.f.n.f)("openwa", raw) {
		t.Fatal("vote observation not admitted")
	}
	if r := f.f.n.f.dispatch(t, "ow:poll-vote-observed", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	var id int64
	var payload []byte
	if err := f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT id,payload FROM jobs WHERE kind='control-notice'").Scan(&id, &payload); err != nil {
		t.Fatal(err)
	}
	var n control.Notice
	if json.Unmarshal(payload, &n) != nil || n.Target.BotID != "master" || n.Target.ChatID != -100 || n.Target.ThreadID != 44 || n.ReplyTo != 2001 || n.Body.Text != "[Poll vote · 12345000000001@lid] B" {
		t.Fatal(n)
	}
	if r := f.f.n.f.run(t, id, "control-notice", f.f.replies.Notice); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	if len(f.f.n.sends) != 0 || f.f.n.reads != 0 {
		t.Fatal("observing vote mutated WhatsApp", f.f.n.sends, f.f.n.reads)
	}
}

func TestPollCrashAfterProviderAcceptanceRepeatsOnlyFrozenSet(t *testing.T) {
	f := newPollFixture(t)
	choices := f.source(t, true)
	if r := f.callback(t, 101, choices[" A "], 44, 2001); r.State != delivery.Done {
		t.Fatal(r)
	}
	ids := f.pending(t)
	j := claim(t, f.f.n.f.store)
	if len(ids) != 1 || j.ID != ids[0] {
		t.Fatal(j, ids)
	}
	p, err := f.f.n.f.store.PreparePollVote(t.Context(), j, polls.Settings{Session: "fixture-session", OwnerID: 123, NoticeTTL: 90})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.f.n.waClient.VotePoll(t.Context(), p.Session, p.Request); err != nil {
		t.Fatal(err)
	}
	// The provider accepted the selection, then the process died before its
	// fenced completion write. No ambiguous message-send effect was created.
	if _, err := f.f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", j.ID); err != nil {
		t.Fatal(err)
	}
	if count, err := f.f.n.f.store.RecoverExpired(t.Context()); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if r := f.run(t, j.ID); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	if len(f.f.n.sends) != 2 || string(f.f.n.sends[0].Request["options"]) != string(f.f.n.sends[1].Request["options"]) {
		t.Fatal(f.f.n.sends)
	}
}
