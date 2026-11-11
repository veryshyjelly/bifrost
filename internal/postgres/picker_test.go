package postgres

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
)

func pickerFixture(t *testing.T, chat, thread int64) *relayFixture {
	t.Helper()
	f := newRelayFixture(t, nil, true)
	f.route(t, mentionGroup, chat, thread)
	f.mu.Lock()
	for i := 0; i < 12; i++ {
		f.groupRows = append(f.groupRows, sdk.GroupParticipant{ID: fmt.Sprintf("12025551%03d@c.us", i), Name: fmt.Sprintf("Person %02d", i)})
	}
	f.mu.Unlock()
	return f
}
func pickerMessage(id int, chat, thread int64, text string) *models.Message {
	m := ownerText(id, chat, text)
	m.MessageThreadID = int(thread)
	m.IsTopicMessage = thread > 0
	return m
}
func admitPicker(t *testing.T, f *relayFixture, bot string, u models.Update) bool {
	t.Helper()
	raw, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	return fixtureIngress(t, f)("telegram/"+bot, raw)
}
func sendPickerCommand(t *testing.T, f *relayFixture, id int64, bot string, chat, thread int64, text string) delivery.Report {
	t.Helper()
	u := models.Update{ID: id, Message: pickerMessage(int(id)+100, chat, thread, text)}
	if !admitPicker(t, f, bot, u) {
		t.Fatal("picker command not admitted")
	}
	return f.dispatch(t, fmt.Sprintf("tg:%s:%d", bot, id), "tg")
}
func pickerMarkup(t *testing.T, c tgTextCall) models.InlineKeyboardMarkup {
	t.Helper()
	var markup models.InlineKeyboardMarkup
	if err := json.Unmarshal([]byte(c.Markup), &markup); err != nil {
		t.Fatal(err)
	}
	return markup
}
func nextPickerAction(t *testing.T, f *relayFixture) string {
	t.Helper()
	f.mu.Lock()
	first := f.tgCalls[0]
	f.mu.Unlock()
	markup := pickerMarkup(t, first)
	for _, row := range markup.InlineKeyboard {
		for _, b := range row {
			if b.Text == "Next" {
				return b.CallbackData
			}
		}
	}
	t.Fatal("missing picker navigation")
	return ""
}
func pickerCallback(id int64, data string, chat, thread int64) models.Update {
	m := &models.Message{ID: 1001, Date: 1700000000, Chat: models.Chat{ID: chat, Type: models.ChatTypeSupergroup}, MessageThreadID: int(thread), IsTopicMessage: thread > 0}
	if chat > 0 {
		m.Chat.Type = models.ChatTypePrivate
	}
	return models.Update{ID: id, CallbackQuery: &models.CallbackQuery{ID: fmt.Sprintf("query-%d", id), From: models.User{ID: 123}, Data: data, Message: models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeMessage, Message: m}}}
}
func TestMentionPickerPagesCopyStableTagsAndRecordExpiryWithoutWhatsAppEffects(t *testing.T) {
	f := pickerFixture(t, -100, 88)
	if r := sendPickerCommand(t, f, 1, "master", -100, 88, "/mention"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	first := f.tgCalls[0]
	f.mu.Unlock()
	markup := pickerMarkup(t, first)
	if !strings.HasPrefix(first.Text, "WhatsApp mentions · page 1/2") || first.Thread != 88 || len(markup.InlineKeyboard) != 11 || markup.InlineKeyboard[0][0].CopyText == nil || first.ParseMode != "" {
		t.Fatal(first)
	}
	var entities []models.MessageEntity
	if err := json.Unmarshal([]byte(first.Entities), &entities); err != nil || len(entities) != 10 || entities[0].Type != models.MessageEntityTypeCode {
		t.Fatal(entities, err)
	}
	copy := markup.InlineKeyboard[0][0].CopyText.Text
	if !strings.HasPrefix(copy, "@{Person 00|") {
		t.Fatal(copy)
	}
	data := nextPickerAction(t, f)
	u := pickerCallback(2, data, -100, 88)
	if !admitPicker(t, f, "master", u) {
		t.Fatal("callback not admitted")
	}
	if r := f.dispatch(t, "tg:master:2", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if admitPicker(t, f, "master", u) {
		t.Fatal("duplicate callback admitted")
	}
	f.mu.Lock()
	second := f.tgCalls[1]
	callbacks := append([]callbackCall(nil), f.callbacks...)
	waCount := len(f.waCalls)
	f.mu.Unlock()
	if !strings.HasPrefix(second.Text, "WhatsApp mentions · page 2/2") || len(pickerMarkup(t, second).InlineKeyboard) != 3 || len(callbacks) != 1 || callbacks[0].Text != "" || f.groupCalls.Load() != 1 || waCount != 0 || f.reads.Load() != 0 {
		t.Fatal(second, callbacks)
	}
	var commandExpiry, replyExpiry int64
	if err := f.store.pool.QueryRow(t.Context(), "SELECT expires_at FROM telegram_observations WHERE message_id=101 AND kind='command'").Scan(&commandExpiry); err != nil {
		t.Fatal(err)
	}
	if err := f.store.pool.QueryRow(t.Context(), "SELECT expires_at FROM telegram_observations WHERE message_id=1001 AND kind='log'").Scan(&replyExpiry); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	if commandExpiry < now+50000 || commandExpiry > now+65000 || replyExpiry < now+590000 || replyExpiry > now+605000 {
		t.Fatal("wrong command/control lifetime", commandExpiry-now, replyExpiry-now)
	}
}
func TestPickerRateRetryRetainsButtonsActionsAndOriginalCommandExpiry(t *testing.T) {
	f := pickerFixture(t, -200, 0)
	f.failTG = 1
	r := sendPickerCommand(t, f, 1, "master", -200, 0, "/mention")
	if r.State != delivery.Pending || r.Delay != time.Minute {
		t.Fatal(r)
	}
	f.mu.Lock()
	first := f.tgCalls[0]
	f.mu.Unlock()
	var before int64
	if err := f.store.pool.QueryRow(t.Context(), "SELECT expires_at FROM telegram_observations WHERE message_id=101").Scan(&before); err != nil {
		t.Fatal(err)
	}
	f.groupFailure.Store(true)
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	r = f.dispatch(t, "tg:master:1", "tg")
	if r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tgCalls) != 2 || f.tgCalls[1] != first || f.groupCalls.Load() != 1 {
		t.Fatal("picker changed on retry", f.tgCalls)
	}
	var after, actions int64
	if err := f.store.pool.QueryRow(t.Context(), "SELECT expires_at FROM telegram_observations WHERE message_id=101").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM control_actions").Scan(&actions); err != nil || actions != 1 || before != after {
		t.Fatal("retry regenerated actions or expiry", actions, before, after, err)
	}
}
func TestExpiredCallbackAcknowledgementDoesNotReplayOrBlockThePreparedPage(t *testing.T) {
	f := pickerFixture(t, -200, 0)
	if r := sendPickerCommand(t, f, 1, "master", -200, 0, "/mention"); r.State != delivery.Done {
		t.Fatal(r)
	}
	data := nextPickerAction(t, f)
	f.answerStatus = 400
	f.failTG = 2
	if !admitPicker(t, f, "master", pickerCallback(2, data, -200, 0)) {
		t.Fatal("callback not admitted")
	}
	r := f.dispatch(t, "tg:master:2", "tg")
	if r.State != delivery.Pending {
		t.Fatal(r)
	}
	f.mu.Lock()
	prepared := f.tgCalls[1]
	f.mu.Unlock()
	f.answerStatus = 503
	f.groupFailure.Store(true)
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	r = f.dispatch(t, "tg:master:2", "tg")
	if r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.callbacks) != 1 || len(f.tgCalls) != 3 || f.tgCalls[2] != prepared {
		t.Fatal("completed acknowledgement or page changed on retry", f.callbacks, f.tgCalls)
	}
	var state string
	if err := f.store.pool.QueryRow(t.Context(), "SELECT state FROM effects WHERE effect_key='tg:master:2:answer'").Scan(&state); err != nil || state != "discarded" {
		t.Fatal(state, err)
	}
}
func TestPickerCallbackIsolationAndUnavailableMessagesOnlyAcknowledgeExpiry(t *testing.T) {
	f := pickerFixture(t, -100, 88)
	if r := sendPickerCommand(t, f, 1, "master", -100, 88, "/mention"); r.State != delivery.Done {
		t.Fatal(r)
	}
	data := nextPickerAction(t, f)
	if err := f.store.PutIngressBot(t.Context(), "fixture-account", ingress.Bot{ID: "200", Username: "alice_bot"}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		bot    string
		update models.Update
	}{
		{"master", pickerCallback(2, data, -100, 89)},
		{"master", pickerCallback(3, data, -300, 0)},
		{"200", pickerCallback(4, data, -100, 88)},
		{"master", pickerCallback(5, data, 123, 0)},
		{"master", pickerCallback(6, "mention:unknown", -100, 88)},
		{"master", models.Update{ID: 7, CallbackQuery: &models.CallbackQuery{ID: "inaccessible", From: models.User{ID: 123}, Data: data, Message: models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeInaccessibleMessage, InaccessibleMessage: &models.InaccessibleMessage{MessageID: 1001, Chat: models.Chat{ID: -100}}}}}},
	}
	for _, c := range cases {
		if !admitPicker(t, f, c.bot, c.update) {
			t.Fatal("callback not admitted")
		}
		if r := f.dispatch(t, fmt.Sprintf("tg:%s:%d", c.bot, c.update.ID), "tg"); r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	wrongOwner := pickerCallback(8, data, -100, 88)
	wrongOwner.CallbackQuery.From.ID = 456
	if admitPicker(t, f, "master", wrongOwner) {
		t.Fatal("foreign owner callback admitted")
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET mode='muted'"); err != nil {
		t.Fatal(err)
	}
	if !admitPicker(t, f, "master", pickerCallback(9, data, -100, 88)) {
		t.Fatal("muted callback not admitted")
	}
	if r := f.dispatch(t, "tg:master:9", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET mode='inbox'"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE control_actions SET payload=jsonb_set(payload,'{ExpiresAt}',to_jsonb($1::bigint))", time.Now().Add(-time.Second).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if !admitPicker(t, f, "master", pickerCallback(10, data, -100, 88)) {
		t.Fatal("expired callback not admitted")
	}
	if r := f.dispatch(t, "tg:master:10", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tgCalls) != 1 || len(f.callbacks) != len(cases)+2 {
		t.Fatal("invalid action created a page", f.tgCalls, f.callbacks)
	}
	for _, a := range f.callbacks {
		if a.Text != "This picker expired. Use /mention again." {
			t.Fatal(a)
		}
	}
	if _, known, err := f.store.ControlAction(t.Context(), "other-account", strings.TrimPrefix(data, "mention:")); err != nil || known {
		t.Fatal("action leaked across accounts", known, err)
	}
}
func TestCachedPickerRosterDoesNotAuthorizeAnUnvalidatedSend(t *testing.T) {
	f := pickerFixture(t, -200, 0)
	if r := sendPickerCommand(t, f, 1, "master", -200, 0, "/mention"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	first := f.tgCalls[0]
	f.mu.Unlock()
	token := pickerMarkup(t, first).InlineKeyboard[0][0].CopyText.Text
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE mention_rosters SET fetched_at=$1,payload=jsonb_set(payload,'{FetchedAt}',to_jsonb($1::bigint))", time.Now().Add(-6*time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	f.groupFailure.Store(true)
	if r := sendPickerCommand(t, f, 2, "master", -200, 0, "/mention Person"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	cached := f.tgCalls[1]
	f.mu.Unlock()
	if !strings.Contains(cached.Text, "Using the cached member list; refresh failed.") {
		t.Fatal(cached)
	}
	raw, _ := json.Marshal(models.Update{ID: 3, Message: ownerText(103, -200, token)})
	if !fixtureIngress(t, f)("telegram/master", raw) {
		t.Fatal("send not admitted")
	}
	if r := f.dispatch(t, "tg:master:3", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.waCalls) != 0 || f.reads.Load() != 0 {
		t.Fatal("stale picker authorized a WhatsApp operation")
	}
}
func TestPickerRepairsLocalReplyTrackingWithoutSendingAgain(t *testing.T) {
	f := pickerFixture(t, -200, 0)
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE telegram_observations ADD CONSTRAINT fixture_reject_log CHECK (kind<>'log')"); err != nil {
		t.Fatal(err)
	}
	r := sendPickerCommand(t, f, 1, "master", -200, 0, "/mention")
	if r.State != delivery.Pending {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE telegram_observations DROP CONSTRAINT fixture_reject_log"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	r = f.dispatch(t, "tg:master:1", "tg")
	if r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tgCalls) != 1 || f.groupCalls.Load() != 1 {
		t.Fatal("reply was sent again after receipt was saved")
	}
}

func TestWholeMirrorPickerIgnoresIncidentalReplyThreadAndCommandAddressing(t *testing.T) {
	f := pickerFixture(t, -200, 0)
	if r := sendPickerCommand(t, f, 1, "master", -200, 0, "/mention@other_bot"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if f.groupCalls.Load() != 0 {
		t.Fatal("unaddressed command fetched a roster")
	}
	if r := sendPickerCommand(t, f, 2, "master", -200, 0, "/MENTION@BiFrOsT_BoT"); r.State != delivery.Done {
		t.Fatal(r)
	}
	data := nextPickerAction(t, f)
	callback := pickerCallback(3, data, -200, 999)
	callback.CallbackQuery.Message.Message.IsTopicMessage = false
	if !admitPicker(t, f, "master", callback) {
		t.Fatal("callback not admitted")
	}
	if r := f.dispatch(t, "tg:master:3", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tgCalls) != 2 || f.tgCalls[1].Thread != 0 || len(f.callbacks) != 1 || f.callbacks[0].Text != "" {
		t.Fatal(f.tgCalls, f.callbacks)
	}
}
func TestPrivateHelpScopesOverlappingReplyIDsByBot(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.perBotIDs = true
	for i, bot := range []string{"200", "201"} {
		if err := f.store.PutIngressBot(t.Context(), "fixture-account", ingress.Bot{ID: bot}); err != nil {
			t.Fatal(err)
		}
		if r := sendPickerCommand(t, f, int64(i+1), bot, 123, 0, "/mention"); r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	var count int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM telegram_observations WHERE message_id=7 AND kind='log' AND scope IN ('200','201') AND expires_at>0").Scan(&count); err != nil || count != 2 {
		t.Fatal("private control replies collided", count, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tgCalls) != 2 || f.groupCalls.Load() != 0 || len(f.waCalls) != 0 || f.reads.Load() != 0 {
		t.Fatal("private help accessed WhatsApp")
	}
	for _, c := range f.tgCalls {
		if !strings.Contains(c.Text, "active WhatsApp group") {
			t.Fatal(c)
		}
	}
}
func TestUnavailableFreshRosterSendsOriginalErrorNotice(t *testing.T) {
	f := pickerFixture(t, -200, 0)
	f.groupFailure.Store(true)
	if r := sendPickerCommand(t, f, 1, "master", -200, 0, "/mention"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tgCalls) != 1 || f.tgCalls[0].Text != "Could not refresh the WhatsApp member list. Try /sync, then send your message again." || f.tgCalls[0].Markup != "" {
		t.Fatal(f.tgCalls)
	}
	var key string
	if err := f.store.pool.QueryRow(t.Context(), "SELECT effect_key FROM effects").Scan(&key); err != nil || key != "tg:master:1:error:text:0" {
		t.Fatal("error notice delivery key changed", key, err)
	}
}
func TestControlTrackingCannotExpireProtectedMessages(t *testing.T) {
	f := pickerFixture(t, -200, 0)
	if _, err := f.store.pool.Exec(t.Context(), "INSERT INTO telegram_observations(account_id,scope,bot_id,chat_id,message_id,kind,message_date,payload) VALUES('fixture-account','*','master',-200,1001,'protected',1,'{}')"); err != nil {
		t.Fatal(err)
	}
	if r := sendPickerCommand(t, f, 1, "master", -200, 0, "/mention"); r.State != delivery.Done {
		t.Fatal(r)
	}
	var kind string
	var expires int64
	if err := f.store.pool.QueryRow(t.Context(), "SELECT kind,expires_at FROM telegram_observations WHERE message_id=1001").Scan(&kind, &expires); err != nil || kind != "protected" || expires != 0 {
		t.Fatal(kind, expires, err)
	}
}

func TestCaptionedMentionCommandWorksWhileEditedCommandsRemainIgnored(t *testing.T) {
	f := pickerFixture(t, -200, 0)
	raw := []byte(`{"update_id":1,"message":{"message_id":101,"date":1700000000,"from":{"id":123},"chat":{"id":-200,"type":"supergroup"},"caption":"/mention Person 01","photo":[{"file_id":"fixture","file_unique_id":"fixture","width":1,"height":1}]}}`)
	if !fixtureIngress(t, f)("telegram/master", raw) {
		t.Fatal("captioned command not admitted")
	}
	if r := f.dispatch(t, "tg:master:1", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	edited := models.Update{ID: 2, EditedMessage: pickerMessage(101, -200, 0, "/mention Person 02")}
	if !admitPicker(t, f, "master", edited) {
		t.Fatal("edited command not admitted")
	}
	if r := f.dispatch(t, "tg:master:2", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	edited = models.Update{ID: 3, EditedMessage: pickerMessage(101, -200, 0, "This was a command")}
	if !admitPicker(t, f, "master", edited) {
		t.Fatal("former command edit not admitted")
	}
	if r := f.dispatch(t, "tg:master:3", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tgCalls) != 1 || !strings.Contains(f.tgCalls[0].Text, "Person 01") || strings.Contains(f.tgCalls[0].Text, "Person 02") || len(f.waCalls) != 0 || f.reads.Load() != 0 {
		t.Fatal("command caption/edit was forwarded", f.tgCalls, f.waCalls)
	}
}

func TestMentionRefusalRetryCannotBecomeAWhatsAppSendAfterTokenChanges(t *testing.T) {
	f := pickerFixture(t, -200, 0)
	f.failTG = 1
	raw, _ := json.Marshal(models.Update{ID: 1, Message: ownerText(101, -200, "Hello @{Unknown|deadbeef}")})
	if !fixtureIngress(t, f)("telegram/master", raw) {
		t.Fatal("owner send not admitted")
	}
	r := f.dispatch(t, "tg:master:1", "tg")
	if r.State != delivery.Pending {
		t.Fatal(r)
	}
	f.mu.Lock()
	notice := f.tgCalls[0]
	f.mu.Unlock()
	if !strings.HasPrefix(notice.Text, "This mention tag is unknown or belongs to another group/account.") {
		t.Fatal(notice)
	}
	if _, err := f.store.pool.Exec(t.Context(), "INSERT INTO mention_tokens(account_id,token,chat_id,person_id) VALUES('fixture-account','deadbeef','123@g.us','12025551000@c.us')"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	r = f.dispatch(t, "tg:master:1", "tg")
	if r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tgCalls) != 2 || f.tgCalls[1] != notice || len(f.waCalls) != 0 || f.reads.Load() != 0 || f.groupCalls.Load() != 0 {
		t.Fatal("refused send was revalidated or sent", f.tgCalls, f.waCalls)
	}
	var kind string
	var expires int64
	if err := f.store.pool.QueryRow(t.Context(), "SELECT kind,expires_at FROM telegram_observations WHERE message_id=101").Scan(&kind, &expires); err != nil || kind != "chat" || expires != 0 {
		t.Fatal("owner message was converted to an expiring notice", kind, expires, err)
	}
	var key string
	if err := f.store.pool.QueryRow(t.Context(), "SELECT effect_key FROM effects").Scan(&key); err != nil || key != "tg:master:1:mention-error:text:0" {
		t.Fatal(key, err)
	}
}
