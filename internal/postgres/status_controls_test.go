package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/bridge"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/statuses"
)

type statusAckCrash struct {
	*Store
	fail bool
}

func (s *statusAckCrash) CompleteStatusCallback(ctx context.Context, j delivery.Job) error {
	if s.fail {
		s.fail = false
		return delivery.ErrLocalRetry
	}
	return s.Store.CompleteStatusCallback(ctx, j)
}

func TestStatusControlsAcknowledgementCrashDoesNotReapplyPreference(t *testing.T) {
	s := newStatusFixture(t)
	if r := s.post(t, "ack-source", "first", "text", "Original", time.Now().Add(time.Hour)); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := s.command(t, 170, "/mute", -100, 99, s.sourceCopy(t, "first")); r.State != delivery.Done {
		t.Fatal(r)
	}
	p, message := s.confirmation(t, 170)
	id := p.Packets[0].Markup.InlineKeyboard[0][0].CallbackData
	store := &statusAckCrash{Store: s.n.f.store, fail: true}
	replies, err := control.New(s.n.f.store, func(_ context.Context, _ string, id string) (control.Telegram, error) { return s.n.tgClients[id], nil }, -100)
	if err != nil {
		t.Fatal(err)
	}
	controls, err := statuses.NewControls(store, replies, func(_ context.Context, _ string, id string) (statuses.ControlTelegram, error) {
		return s.n.tgClients[id], nil
	}, s.s, s.c)
	if err != nil {
		t.Fatal(err)
	}
	options := s.n.f.dispatchOptions
	options.Extensions.PassiveTopics = controls.Handle
	s.n.f.dispatcher, err = bridge.New(s.n.f.store, s.n.f.service, s.n.f.whatsapp, options)
	if err != nil {
		t.Fatal(err)
	}
	first := s.callback(t, 171, id, -100, 99, message)
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	if current := s.effectiveMute(t, "12345000000001@lid"); current.Active || current.Revision != 2 {
		t.Fatal(current)
	}
	if _, err := s.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", first.JobID); err != nil {
		t.Fatal(err)
	}
	if r := s.n.f.dispatch(t, "tg:master:171", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if current := s.effectiveMute(t, "12345000000001@lid"); current.Active || current.Revision != 2 {
		t.Fatal("ack replay changed revision", current)
	}
	if len(s.editJobs(t)) != 1 {
		t.Fatal("ack retry queued another edit")
	}
	var effects int
	if err := s.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM effects WHERE job_id=$1", first.JobID).Scan(&effects); err != nil || effects != 0 {
		t.Fatal(effects, err)
	}
}

func TestStatusControlsMemberBotCannotUseMasterControl(t *testing.T) {
	s := newStatusFixture(t)
	if r := s.post(t, "member-source", "first", "text", "Original", time.Now().Add(time.Hour)); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := s.command(t, 180, "/mute", -100, 99, s.sourceCopy(t, "first")); r.State != delivery.Done {
		t.Fatal(r)
	}
	p, message := s.confirmation(t, 180)
	id := p.Packets[0].Markup.InlineKeyboard[0][0].CallbackData
	s.n.f.bind(t, "200", "2@s.whatsapp.net")
	u := models.Update{ID: 181, CallbackQuery: &models.CallbackQuery{ID: "181", From: models.User{ID: 123}, Data: id, Message: models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeMessage, Message: pickerMessage(message, -100, 99, "")}}}
	if !admitPicker(t, s.n.f, "200", u) {
		t.Fatal("member callback not admitted")
	}
	if r := s.n.f.dispatch(t, "tg:200:181", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if !s.effectiveMute(t, "12345000000001@lid").Active || len(s.editJobs(t)) != 0 {
		t.Fatal("member bot changed master preference")
	}
}

func (s *statusFixture) command(t *testing.T, update int64, text string, chat, thread int64, reply int) delivery.Report {
	t.Helper()
	m := pickerMessage(int(update), chat, thread, text)
	if reply > 0 {
		m.ReplyToMessage = &models.Message{ID: reply}
	}
	u := models.Update{ID: update, Message: m}
	if !admitPicker(t, s.n.f, "master", u) {
		t.Fatal("not admitted")
	}
	return s.n.f.dispatch(t, "tg:master:"+strconv.FormatInt(update, 10), "tg")
}
func (s *statusFixture) callback(t *testing.T, update int64, id string, chat, thread int64, message int) delivery.Report {
	t.Helper()
	m := pickerMessage(message, chat, thread, "")
	u := models.Update{ID: update, CallbackQuery: &models.CallbackQuery{ID: fmt.Sprint(update), From: models.User{ID: 123}, Data: id, Message: models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeMessage, Message: m}}}
	if !admitPicker(t, s.n.f, "master", u) {
		t.Fatal("callback not admitted")
	}
	return s.n.f.dispatch(t, "tg:master:"+strconv.FormatInt(update, 10), "tg")
}
func (s *statusFixture) confirmation(t *testing.T, update int64) (control.Plan, int) {
	t.Helper()
	key := "tg:master:" + strconv.FormatInt(update, 10)
	p, found, err := s.n.f.store.ControlPlan(t.Context(), "fixture-account", key)
	if err != nil || !found {
		t.Fatal(p, found, err)
	}
	effect, err := s.n.f.store.queries.GetEffect(t.Context(), db.GetEffectParams{AccountID: "fixture-account", EffectKey: p.PacketKey(0)})
	if err != nil {
		t.Fatal(err)
	}
	var message models.Message
	if json.Unmarshal(effect.Result, &message) != nil {
		t.Fatal("receipt invalid")
	}
	return p, message.ID
}
func (s *statusFixture) effectiveMute(t *testing.T, author string) statuses.Mute {
	t.Helper()
	d, err := directory(t.Context(), s.n.f.store.queries, "fixture-account")
	if err != nil {
		t.Fatal(err)
	}
	m, err := statusMutes(t.Context(), s.n.f.store.queries, "fixture-account", d)
	if err != nil {
		t.Fatal(err)
	}
	return m[d.Canonical(author)]
}
func (s *statusFixture) sourceCopy(t *testing.T, id string) int {
	t.Helper()
	var message int
	if err := s.n.f.store.pool.QueryRow(t.Context(), "SELECT telegram_message_id FROM message_mappings WHERE whatsapp_message_id=$1 LIMIT 1", id).Scan(&message); err != nil {
		t.Fatal(err)
	}
	return message
}
func (s *statusFixture) editJobs(t *testing.T) []int64 {
	t.Helper()
	rows, err := s.n.f.store.pool.Query(t.Context(), "SELECT id FROM jobs WHERE kind='control-status-edit' AND state='pending' ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestStatusControlsMuteAliasesPersistSkipAndNeverMuteChat(t *testing.T) {
	s := newStatusFixture(t)
	if r := s.post(t, "source-mute", "first", "text", "Original", time.Now().Add(time.Hour)); r.State != delivery.Done {
		t.Fatal(r)
	}
	copy := s.sourceCopy(t, "first")
	if r := s.command(t, 100, "/mute", -100, 99, copy); r.State != delivery.Done {
		t.Fatal(r)
	}
	first := s.effectiveMute(t, "12345000000001@lid")
	if !first.Active || first.Revision != 1 {
		t.Fatal(first)
	}
	if r := s.post(t, "muted-arrival", "skipped", "text", "Skip", time.Now().Add(time.Hour)); r.State != delivery.Done {
		t.Fatal(r)
	}
	if _, err := s.n.f.store.pool.Exec(t.Context(), "INSERT INTO contact_aliases(account_id,alias,canonical) VALUES('fixture-account','12345000000001@lid','12025550123@c.us')"); err != nil {
		t.Fatal(err)
	}
	if r := s.command(t, 101, "/keep", -100, 99, copy); r.State != delivery.Done {
		t.Fatal(r)
	}
	second := s.effectiveMute(t, "12345000000001@lid")
	if second.Active || second.Revision != 2 || second.Author != "12025550123@c.us" {
		t.Fatal(second)
	}
	if r := s.post(t, "unmuted-old", "skipped", "text", "Do not backfill", time.Now().Add(time.Hour)); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := s.post(t, "unmuted-new", "next", "text", "New post", time.Now().Add(time.Hour)); r.State != delivery.Done {
		t.Fatal(r)
	}
	var skipped, copies, routes int
	if err := s.n.f.store.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM status_posts WHERE skipped),(SELECT count(*) FROM message_mappings),(SELECT count(*) FROM bridge_routes)").Scan(&skipped, &copies, &routes); err != nil || skipped != 1 || copies != 2 || routes != 0 {
		t.Fatal(skipped, copies, routes, err)
	}
	if _, err := s.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET state='pending',due=clock_timestamp() WHERE dedup='tg:master:100'"); err != nil {
		t.Fatal(err)
	}
	if r := s.n.f.dispatch(t, "tg:master:100", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if current := s.effectiveMute(t, "12345000000001@lid"); current.Active || current.Revision != 2 {
		t.Fatal("notice replay reapplied mute", current)
	}
}

func TestStatusControlsStaleUnmuteAndWrongTopicCannotReverseNewMute(t *testing.T) {
	s := newStatusFixture(t)
	if r := s.post(t, "source-stale", "first", "text", "Original", time.Now().Add(time.Hour)); r.State != delivery.Done {
		t.Fatal(r)
	}
	copy := s.sourceCopy(t, "first")
	if r := s.command(t, 110, "/mute", -100, 99, copy); r.State != delivery.Done {
		t.Fatal(r)
	}
	p, message := s.confirmation(t, 110)
	id := p.Packets[0].Markup.InlineKeyboard[0][0].CallbackData
	if r := s.callback(t, 111, id, -100, 98, message); r.State != delivery.Done {
		t.Fatal(r)
	}
	if !s.effectiveMute(t, "12345000000001@lid").Active {
		t.Fatal("wrong topic changed preference")
	}
	if r := s.command(t, 112, "/mute", -100, 99, copy); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := s.callback(t, 113, id, -100, 99, message); r.State != delivery.Done {
		t.Fatal(r)
	}
	current := s.effectiveMute(t, "12345000000001@lid")
	if !current.Active || current.Revision != 2 || len(s.editJobs(t)) != 0 {
		t.Fatal(current)
	}
	p, _, err := s.n.f.store.ControlPlan(t.Context(), "fixture-account", "tg:master:113")
	if err != nil || p.Answer == nil || !strings.Contains(p.Answer.Text, "changed") {
		t.Fatal(p, err)
	}
}

func TestStatusControlsValidUnmuteEditsOriginalWithIdempotentRecovery(t *testing.T) {
	s := newStatusFixture(t)
	if r := s.post(t, "source-valid", "first", "text", "Original", time.Now().Add(time.Hour)); r.State != delivery.Done {
		t.Fatal(r)
	}
	copy := s.sourceCopy(t, "first")
	if r := s.command(t, 120, "/mute", -100, 99, copy); r.State != delivery.Done {
		t.Fatal(r)
	}
	p, message := s.confirmation(t, 120)
	id := p.Packets[0].Markup.InlineKeyboard[0][0].CallbackData
	if r := s.callback(t, 121, id, -100, 99, message); r.State != delivery.Done {
		t.Fatal(r)
	}
	if current := s.effectiveMute(t, "12345000000001@lid"); current.Active || current.Revision != 2 {
		t.Fatal(current)
	}
	jobs := s.editJobs(t)
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	s.n.mu.Lock()
	s.n.dropMethod = "editMessageText"
	s.n.mu.Unlock()
	if r := s.n.f.run(t, jobs[0], "control-status-edit", s.controls.Edit); r.State != delivery.Pending {
		t.Fatal(r)
	}
	s.n.mu.Lock()
	s.n.dropMethod = ""
	s.n.mu.Unlock()
	if _, err := s.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", jobs[0]); err != nil {
		t.Fatal(err)
	}
	if r := s.n.f.run(t, jobs[0], "control-status-edit", s.controls.Edit); r.State != delivery.Done {
		t.Fatal(r)
	}
	var effects int
	if err := s.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM effects WHERE job_id=$1", jobs[0]).Scan(&effects); err != nil || effects != 0 {
		t.Fatal(effects, err)
	}
	s.n.mu.Lock()
	defer s.n.mu.Unlock()
	last := s.n.calls[len(s.n.calls)-1]
	if last.Method != "editMessageText" || last.MessageID != message || !strings.Contains(last.Text, "Unmuted") {
		t.Fatal(last)
	}
}

func TestStatusControlsPassiveTextEditsAndReactionsNeverReachWhatsApp(t *testing.T) {
	s := newStatusFixture(t)
	if r := s.post(t, "passive-source", "first", "text", "Original", time.Now().Add(time.Hour)); r.State != delivery.Done {
		t.Fatal(r)
	}
	copy := s.sourceCopy(t, "first")
	if r := s.command(t, 130, "An ordinary reply", -100, 99, copy); r.State != delivery.Done {
		t.Fatal(r)
	}
	u := models.Update{ID: 131, EditedMessage: pickerMessage(130, -100, 99, "Edited reply")}
	if !admitPicker(t, s.n.f, "master", u) {
		t.Fatal("edit not admitted")
	}
	if r := s.n.f.dispatch(t, "tg:master:131", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	u = models.Update{ID: 132, MessageReaction: &models.MessageReactionUpdated{Chat: models.Chat{ID: -100, Type: models.ChatTypeSupergroup}, MessageID: copy, User: &models.User{ID: 123}, NewReaction: []models.ReactionType{standardReaction("👍")}}}
	if !admitPicker(t, s.n.f, "master", u) {
		t.Fatal("reaction not admitted")
	}
	if r := s.n.f.dispatch(t, "tg:master:132", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	s.n.mu.Lock()
	defer s.n.mu.Unlock()
	if len(s.n.sends) != 0 || len(s.n.calls) != 3 || !strings.Contains(s.n.calls[2].Text, "archives WhatsApp Status") {
		t.Fatal(s.n.calls, s.n.sends)
	}
	if len(s.n.f.waCalls) != 0 || s.n.f.reads.Load() != 0 {
		t.Fatal("passive archive reached WhatsApp")
	}
}

func TestStatusControlsPaginationRequiresConfirmedViewAndPreservesRefusedControls(t *testing.T) {
	s := newStatusFixture(t)
	for i := 1; i <= 12; i++ {
		m := statuses.Mute{Author: fmt.Sprintf("%d@c.us", 100+i), Name: fmt.Sprintf("Person %02d", i), Active: true, Revision: int64(i)}
		raw, _ := json.Marshal(m)
		if err := s.n.f.store.queries.SaveStatusMute(t.Context(), db.SaveStatusMuteParams{AccountID: "fixture-account", Author: m.Author, Payload: raw}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.n.f.store.pool.Exec(t.Context(), "INSERT INTO status_mute_counters(account_id,revision) VALUES('fixture-account',12)"); err != nil {
		t.Fatal(err)
	}
	if r := s.command(t, 140, "/muted_statuses 2", 123, 0, 0); r.State != delivery.Done {
		t.Fatal(r)
	}
	p, message := s.confirmation(t, 140)
	if !strings.Contains(p.Packets[0].Text, "page 2/2") || len(p.Packets[0].Markup.InlineKeyboard) != 3 {
		t.Fatal(p.Packets)
	}
	id := p.Packets[0].Markup.InlineKeyboard[0][0].CallbackData
	if r := s.callback(t, 141, id, 123, 0, message); r.State != delivery.Done {
		t.Fatal(r)
	}
	jobs := s.editJobs(t)
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	raw, err := s.n.f.store.queries.StatusControlEdit(t.Context(), db.StatusControlEditParams{AccountID: "fixture-account", JobID: jobs[0]})
	if err != nil {
		t.Fatal(err)
	}
	var edit statuses.Edit
	if json.Unmarshal(raw.Payload, &edit) != nil {
		t.Fatal("bad edit")
	}
	if !strings.Contains(edit.Body.Text, "11 people, page 2/2") || len(edit.Markup.InlineKeyboard) != 2 {
		t.Fatal(edit)
	}
	newID := edit.Markup.InlineKeyboard[0][0].CallbackData
	if r := s.callback(t, 142, newID, 123, 0, message); r.State != delivery.Done {
		t.Fatal(r)
	}
	if current := s.effectiveMute(t, "112@c.us"); !current.Active {
		t.Fatal("unpresented view changed mute", current)
	}
	s.n.mu.Lock()
	s.n.failMethod, s.n.failCode = "editMessageText", 403
	s.n.mu.Unlock()
	if r := s.n.f.run(t, jobs[0], "control-status-edit", s.controls.Edit); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := s.callback(t, 143, newID, 123, 0, message); r.State != delivery.Done {
		t.Fatal(r)
	}
	if current := s.effectiveMute(t, "112@c.us"); !current.Active {
		t.Fatal("refused view changed mute", current)
	}
}

func TestStatusControlsExpiryAndOriginalMessageReceiptAreRequired(t *testing.T) {
	s := newStatusFixture(t)
	if r := s.post(t, "expiry-source", "first", "text", "Original", time.Now().Add(time.Hour)); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := s.command(t, 150, "/mute_status", -100, 99, s.sourceCopy(t, "first")); r.State != delivery.Done {
		t.Fatal(r)
	}
	p, message := s.confirmation(t, 150)
	id := p.Packets[0].Markup.InlineKeyboard[0][0].CallbackData
	if r := s.callback(t, 151, id, -100, 99, message+100); r.State != delivery.Done {
		t.Fatal(r)
	}
	if !s.effectiveMute(t, "12345000000001@lid").Active {
		t.Fatal("foreign message changed mute")
	}
	if _, err := s.n.f.store.pool.Exec(t.Context(), "UPDATE status_controls SET payload=jsonb_set(payload,'{ExpiresAt}','1'::jsonb) WHERE control_id=$1", strings.TrimPrefix(id, "statusctl:")); err != nil {
		t.Fatal(err)
	}
	if r := s.callback(t, 152, id, -100, 99, message); r.State != delivery.Done {
		t.Fatal(r)
	}
	if !s.effectiveMute(t, "12345000000001@lid").Active || len(s.editJobs(t)) != 0 {
		t.Fatal("expired action changed mute")
	}
}

func TestStatusControlsRefreshCommandCreatesFeedAndQueuesOnce(t *testing.T) {
	s := newStatusFixture(t)
	if r := s.command(t, 160, "/statuses", 123, 0, 0); r.State != delivery.Done {
		t.Fatal(r)
	}
	p, _ := s.confirmation(t, 160)
	if p.Packets[0].Markup == nil || !strings.HasSuffix(p.Packets[0].Markup.InlineKeyboard[0][0].URL, "/99") {
		t.Fatal(p.Packets)
	}
	var id int64
	if err := s.n.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='status-refresh'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if r := s.n.f.run(t, id, "status-refresh", s.s.Refresh); r.State != delivery.Done {
		t.Fatal(r)
	}
	if _, err := s.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET state='pending',due=clock_timestamp() WHERE dedup='tg:master:160'"); err != nil {
		t.Fatal(err)
	}
	if r := s.n.f.dispatch(t, "tg:master:160", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	var queued int
	if err := s.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='status-refresh'").Scan(&queued); err != nil || queued != 1 {
		t.Fatal(queued, err)
	}
	s.n.mu.Lock()
	defer s.n.mu.Unlock()
	if len(s.n.calls) != 2 || s.n.statusLists != 1 || len(s.n.sends) != 0 {
		t.Fatal(s.n.calls, s.n.statusLists)
	}
}
