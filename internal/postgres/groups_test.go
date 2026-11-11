package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

func groupButton(t *testing.T, f *relayFixture, id int64) string {
	t.Helper()
	if r := operatorCommand(t, f, id, "master", -100, 44, "/group"); r.State != delivery.Done {
		t.Fatal(r)
	}
	markup := pickerMarkup(t, f.tgCalls[len(f.tgCalls)-1])
	u, err := url.Parse(markup.InlineKeyboard[0][0].URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("start")
}
func groupSelector(t *testing.T, f *relayFixture, id int64, parameter string) int {
	t.Helper()
	if r := operatorCommand(t, f, id, "master", 123, 0, "/start "+parameter); r.State != delivery.Done {
		t.Fatal(r)
	}
	var markup struct {
		Keyboard [][]struct {
			Request map[string]json.RawMessage `json:"request_chat"`
		} `json:"keyboard"`
		Resize, Once bool
	}
	if err := json.Unmarshal([]byte(f.tgCalls[len(f.tgCalls)-1].Markup), &markup); err != nil {
		t.Fatal(err)
	}
	if len(markup.Keyboard) != 1 {
		t.Fatal("no native selector", f.tgCalls)
	}
	r := markup.Keyboard[0][0].Request
	var requestID int
	if err := json.Unmarshal(r["request_id"], &requestID); err != nil {
		t.Fatal(err)
	}
	if requestID <= 0 || string(r["chat_has_username"]) != "false" || string(r["chat_is_channel"]) != "false" || string(r["chat_is_created"]) != "true" || string(r["bot_is_member"]) != "true" || string(r["request_title"]) != "true" || string(r["user_administrator_rights"]) != string(r["bot_administrator_rights"]) {
		t.Fatal("native picker constraints changed", string(f.tgCalls[len(f.tgCalls)-1].Markup))
	}
	var rights map[string]bool
	if err := json.Unmarshal(r["bot_administrator_rights"], &rights); err != nil {
		t.Fatal(err)
	}
	if !rights["can_delete_messages"] || !rights["can_manage_chat"] || !rights["can_change_info"] || !rights["can_invite_users"] || rights["can_promote_members"] {
		t.Fatal(rights)
	}
	return requestID
}
func shareGroup(t *testing.T, f *relayFixture, id int64, request int, target int64) delivery.Report {
	t.Helper()
	m := pickerMessage(int(id)+10000, 123, 0, "")
	m.ChatShared = &models.ChatShared{RequestID: request, ChatID: target, Title: "Selected"}
	if !admitPicker(t, f, "master", models.Update{ID: id, Message: m}) {
		t.Fatal("selection not admitted")
	}
	return f.dispatch(t, fmt.Sprintf("tg:master:%d", id), "tg")
}
func headerJob(t *testing.T, f *relayFixture, key, chat string, thread int64) string {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"type": "new_conversation", "data": map[string]any{"chatId": chat, "tgChat": int64(-100), "thread": thread, "returnKey": ""}})
	if _, err := f.store.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: key, Stream: key, Kind: "management", Payload: b}); err != nil {
		t.Fatal(err)
	}
	return key
}
func headerCallback(t *testing.T, f *relayFixture, id int64, data string, chat, thread int64) delivery.Report {
	t.Helper()
	u := pickerCallback(id, data, chat, thread)
	if !admitPicker(t, f, "master", u) {
		t.Fatal("callback not admitted")
	}
	return f.dispatch(t, fmt.Sprintf("tg:master:%d", id), "tg")
}

func TestGroupPickerNativeSelectionLinksOnlyAfterOwnerSharesGroup(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "group@g.us", -100, 44)
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET name='Friends'"); err != nil {
		t.Fatal(err)
	}
	parameter := groupButton(t, f, 3000)
	request := groupSelector(t, f, 3001, parameter)
	if chat, _, _ := mirrorRoute(t, f, "group@g.us"); chat != -100 {
		t.Fatal(chat)
	}
	if r := shareGroup(t, f, 3002, request, -200); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 3002), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if chat, thread, mode := mirrorRoute(t, f, "group@g.us"); chat != -200 || thread != 0 || mode != "group" {
		t.Fatal(chat, thread, mode)
	}
	last := f.tgCalls[len(f.tgCalls)-1]
	if last.Chat != 123 || last.Text != "Linked Friends to its Telegram group. You can now chat there." || last.Markup != `{"remove_keyboard":true}` {
		t.Fatal(last)
	}
	var used bool
	if err := f.store.pool.QueryRow(t.Context(), "SELECT used_job_id IS NOT NULL FROM mirror_chat_requests WHERE request_id=$1", request).Scan(&used); err != nil || !used {
		t.Fatal(used, err)
	}
	if r := shareGroup(t, f, 3003, request, -300); r.State != delivery.Done {
		t.Fatal(r)
	}
	if f.tgCalls[len(f.tgCalls)-1].Text != control.GroupSelectionExpired {
		t.Fatal(f.tgCalls)
	}
	if len(f.waCalls) != 0 || f.reads.Load() != 0 {
		t.Fatal("selection touched WhatsApp")
	}
}
func TestGroupPickerRejectsExpiredForeignAccountChangedRouteAndUsedRequests(t *testing.T) {
	for _, kind := range []string{"expiry", "account", "owner", "route", "return generation"} {
		t.Run(kind, func(t *testing.T) {
			f := mirrorFixture(t)
			f.route(t, "group@g.us", -100, 44)
			parameter := groupButton(t, f, 3010)
			choice := strings.TrimPrefix(parameter, "mirror_")
			switch kind {
			case "expiry":
				if _, err := f.store.pool.Exec(t.Context(), "UPDATE mirror_choices SET payload=jsonb_set(payload,'{ExpiresAt}','0') WHERE choice_id=$1", choice); err != nil {
					t.Fatal(err)
				}
			case "account":
				if _, err := f.store.pool.Exec(t.Context(), `UPDATE mirror_choices SET payload=jsonb_set(payload,'{AccountID}','"other-account"') WHERE choice_id=$1`, choice); err != nil {
					t.Fatal(err)
				}
			case "owner":
				if _, err := f.store.pool.Exec(t.Context(), `UPDATE mirror_choices SET payload=jsonb_set(payload,'{OwnerID}','999') WHERE choice_id=$1`, choice); err != nil {
					t.Fatal(err)
				}
			case "route":
				f.route(t, "group@g.us", -100, 55)
			case "return generation":
				if _, err := f.store.pool.Exec(t.Context(), "INSERT INTO mirror_returns(account_id,whatsapp_chat,return_key) VALUES('fixture-account','group@g.us','new-generation')"); err != nil {
					t.Fatal(err)
				}
			}
			if r := operatorCommand(t, f, 3011, "master", 123, 0, "/start "+parameter); r.State != delivery.Done {
				t.Fatal(r)
			}
			if last := f.tgCalls[len(f.tgCalls)-1]; last.Text != control.GroupButtonExpired || last.Markup != "" {
				t.Fatal(last)
			}
			var requests int
			if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM mirror_chat_requests").Scan(&requests); err != nil || requests != 0 {
				t.Fatal(requests, err)
			}
		})
	}
}
func TestGroupPickerPublicAndManagementDestinationsCannotConsumeRequest(t *testing.T) {
	for _, kind := range []string{"public", "management"} {
		t.Run(kind, func(t *testing.T) {
			f := mirrorFixture(t)
			f.route(t, "group@g.us", -100, 44)
			request := groupSelector(t, f, 3021, groupButton(t, f, 3020))
			target := int64(-200)
			if kind == "public" {
				f.chatUsername = "public_group"
			} else {
				target = -100
			}
			if r := shareGroup(t, f, 3022, request, target); r.State != delivery.Done {
				t.Fatal(r)
			}
			if r := f.dispatch(t, operatorKey("master", 3022), "management"); r.State != delivery.Failed {
				t.Fatal(r)
			}
			var used bool
			if err := f.store.pool.QueryRow(t.Context(), "SELECT used_job_id IS NOT NULL FROM mirror_chat_requests").Scan(&used); err != nil || used {
				t.Fatal(used, err)
			}
			if chat, _, _ := mirrorRoute(t, f, "group@g.us"); chat != -100 {
				t.Fatal(chat)
			}
		})
	}
}
func TestGroupPickerReplyRetryFreezesRequestAndLinkConfirmationReceipts(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "group@g.us", -100, 44)
	parameter := groupButton(t, f, 3030)
	f.failTG = 2
	if r := operatorCommand(t, f, 3031, "master", 123, 0, "/start "+parameter); r.State != delivery.Pending {
		t.Fatal(r)
	}
	dueMirror(t, f, "tg:master:3031")
	if r := f.dispatch(t, "tg:master:3031", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if f.tgCalls[1] != f.tgCalls[2] {
		t.Fatal("selector changed on retry")
	}
	var request int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT request_id FROM mirror_chat_requests").Scan(&request); err != nil {
		t.Fatal(err)
	}
	f.failTG = 5 // the private completion fails after the group receipt succeeds
	if r := shareGroup(t, f, 3032, request, -200); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 3032), "management"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	n := len(f.tgCalls)
	f.route(t, "group@g.us", -300, 0)
	dueMirror(t, f, operatorKey("master", 3032))
	if r := f.dispatch(t, operatorKey("master", 3032), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(f.tgCalls) != n+1 || f.tgCalls[n] != f.tgCalls[n-1] || f.tgCalls[n].Chat != 123 {
		t.Fatal(f.tgCalls)
	}
	if chat, _, _ := mirrorRoute(t, f, "group@g.us"); chat != -300 {
		t.Fatal("completion reapplied link", chat)
	}
}
func TestGroupPickerConsumptionRollsBackWithoutCompleteLinkPlan(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "group@g.us", -100, 44)
	request := groupSelector(t, f, 3041, groupButton(t, f, 3040))
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans ADD CONSTRAINT fixture_refusal CHECK (plan_key NOT LIKE 'management:%')"); err != nil {
		t.Fatal(err)
	}
	if r := shareGroup(t, f, 3042, request, -200); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 3042), "management"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	var used bool
	if err := f.store.pool.QueryRow(t.Context(), "SELECT used_job_id IS NOT NULL FROM mirror_chat_requests").Scan(&used); err != nil || used {
		t.Fatal(used, err)
	}
	if chat, _, _ := mirrorRoute(t, f, "group@g.us"); chat != -100 {
		t.Fatal(chat)
	}
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans DROP CONSTRAINT fixture_refusal"); err != nil {
		t.Fatal(err)
	}
	dueMirror(t, f, operatorKey("master", 3042))
	if r := f.dispatch(t, operatorKey("master", 3042), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
}

func TestGroupPickerRevalidatesExpiryBeforeLinkCommit(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "group@g.us", -100, 44)
	request := groupSelector(t, f, 3046, groupButton(t, f, 3045))
	if r := shareGroup(t, f, 3047, request, -200); r.State != delivery.Done {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE mirror_chat_requests SET payload=jsonb_set(payload,'{ExpiresAt}','0')"); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, operatorKey("master", 3047), "management"); r.State != delivery.Failed {
		t.Fatal(r)
	}
	if chat, _, _ := mirrorRoute(t, f, "group@g.us"); chat != -100 {
		t.Fatal(chat)
	}
}

func TestGroupPickerUnknownRequestsAndOtherOwnersDoNotLink(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "group@g.us", -100, 44)
	request := groupSelector(t, f, 3049, groupButton(t, f, 3048))
	m := pickerMessage(77, 999, 0, "")
	m.From.ID = 999
	m.ChatShared = &models.ChatShared{RequestID: request, ChatID: -200}
	if admitPicker(t, f, "master", models.Update{ID: 3059, Message: m}) {
		t.Fatal("foreign owner was admitted")
	}
	if r := shareGroup(t, f, 3058, 0, -200); r.State != delivery.Done {
		t.Fatal(r)
	}
	if f.tgCalls[len(f.tgCalls)-1].Text != control.GroupSelectionExpired {
		t.Fatal(f.tgCalls)
	}
	if chat, _, _ := mirrorRoute(t, f, "group@g.us"); chat != -100 {
		t.Fatal(chat)
	}
}

func TestGroupPickerLostPromptResponseRemainsUncertain(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "group@g.us", -100, 44)
	parameter := groupButton(t, f, 3055)
	f.dropTGAt = 2
	if r := operatorCommand(t, f, 3056, "master", 123, 0, "/start "+parameter); r.State != delivery.Uncertain {
		t.Fatal(r)
	}
	var requests int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM mirror_chat_requests").Scan(&requests); err != nil || requests != 1 {
		t.Fatal(requests, err)
	}
	if chat, _, _ := mirrorRoute(t, f, "group@g.us"); chat != -100 {
		t.Fatal(chat)
	}
}
func TestTopicHeaderRestoresGroupControlsAndScopedOwnerActions(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "group@g.us", -100, 44)
	if r := f.dispatch(t, headerJob(t, f, "header", "group@g.us", 44), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	markup := pickerMarkup(t, f.tgCalls[0])
	if len(markup.InlineKeyboard) != 3 || markup.InlineKeyboard[0][0].Text != "Create / link Telegram group" || markup.InlineKeyboard[1][0].Text != "Keep here" {
		t.Fatal(markup)
	}
	mute := markup.InlineKeyboard[1][1].CallbackData
	if r := headerCallback(t, f, 3050, mute, -100, 44); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 3050), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if operatorMode(t, f, "group@g.us") != "muted" || len(f.callbacks) != 1 {
		t.Fatal(f.callbacks)
	}
	keep := markup.InlineKeyboard[1][0].CallbackData
	if r := headerCallback(t, f, 3051, keep, -100, 44); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 3051), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if operatorMode(t, f, "group@g.us") != "inbox" {
		t.Fatal("paused controls could not resume")
	}
}
func TestTopicCallbacksRejectWrongTopicTamperingExpiryAndGeneration(t *testing.T) {
	for _, kind := range []string{"topic", "operation", "expiry", "account", "generation", "inaccessible"} {
		t.Run(kind, func(t *testing.T) {
			f := mirrorFixture(t)
			f.route(t, "group@g.us", -100, 44)
			if r := f.dispatch(t, headerJob(t, f, "header", "group@g.us", 44), "management"); r.State != delivery.Done {
				t.Fatal(r)
			}
			data := pickerMarkup(t, f.tgCalls[0]).InlineKeyboard[1][1].CallbackData
			u := pickerCallback(3060, data, -100, 44)
			switch kind {
			case "topic":
				u = pickerCallback(3060, data, -100, 55)
			case "operation":
				u.CallbackQuery.Data = strings.Replace(data, "mute:", "read:", 1)
			case "expiry":
				if _, err := f.store.pool.Exec(t.Context(), "UPDATE control_actions SET payload=jsonb_set(payload,'{ExpiresAt}','0')"); err != nil {
					t.Fatal(err)
				}
			case "account":
				if _, err := f.store.pool.Exec(t.Context(), `UPDATE control_actions SET payload=jsonb_set(payload,'{AccountID}','"other-account"')`); err != nil {
					t.Fatal(err)
				}
			case "generation":
				if _, err := f.store.pool.Exec(t.Context(), "INSERT INTO mirror_returns(account_id,whatsapp_chat,return_key) VALUES('fixture-account','group@g.us','changed')"); err != nil {
					t.Fatal(err)
				}
			case "inaccessible":
				u.CallbackQuery.Message = models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeInaccessibleMessage, InaccessibleMessage: &models.InaccessibleMessage{Chat: models.Chat{ID: -100}, MessageID: 1001}}
			}
			if !admitPicker(t, f, "master", u) {
				t.Fatal("callback not admitted")
			}
			if r := f.dispatch(t, "tg:master:3060", "tg"); r.State != delivery.Done {
				t.Fatal(r)
			}
			var n int
			if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE dedup=$1", operatorKey("master", 3060)).Scan(&n); err != nil || n != 0 || operatorMode(t, f, "group@g.us") != "inbox" {
				t.Fatal(n, err)
			}
		})
	}
}
func TestTopicHeaderObsoleteSnapshotDoesNotPostIntoLaterRoute(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "group@g.us", -100, 44)
	f.failTG = 1
	key := headerJob(t, f, "header", "group@g.us", 44)
	if r := f.dispatch(t, key, "management"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	f.route(t, "group@g.us", -200, 0)
	dueMirror(t, f, key)
	if r := f.dispatch(t, key, "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(f.tgCalls) != 1 {
		t.Fatal("obsolete header was resent", f.tgCalls)
	}
	key = headerJob(t, f, "stale-header", "group@g.us", 44)
	if r := f.dispatch(t, key, "management"); r.State != delivery.Done || len(f.tgCalls) != 1 {
		t.Fatal(r, f.tgCalls)
	}
}

func TestTopicHeaderFailureRollsBackActionsAndReconcilesConfirmedReceipt(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "group@g.us", -100, 44)
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans ADD CONSTRAINT fixture_refusal CHECK (plan_key<>'header')"); err != nil {
		t.Fatal(err)
	}
	key := headerJob(t, f, "header", "group@g.us", 44)
	if r := f.dispatch(t, key, "management"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	var choices, actions int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM mirror_choices").Scan(&choices); err != nil {
		t.Fatal(err)
	}
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM control_actions").Scan(&actions); err != nil || choices != 0 || actions != 0 {
		t.Fatal(choices, actions, err)
	}
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans DROP CONSTRAINT fixture_refusal; ALTER TABLE telegram_observations ADD CONSTRAINT fixture_recording CHECK (kind<>'log')"); err != nil {
		t.Fatal(err)
	}
	dueMirror(t, f, key)
	if r := f.dispatch(t, key, "management"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE telegram_observations DROP CONSTRAINT fixture_recording"); err != nil {
		t.Fatal(err)
	}
	dueMirror(t, f, key)
	if r := f.dispatch(t, key, "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(f.tgCalls) != 1 {
		t.Fatal("header receipt was resent", f.tgCalls)
	}
}

func TestContactTopicHeaderRetainsBotCreationControlWithoutMirrorButton(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "12025550100@c.us", -100, 44)
	if r := f.dispatch(t, headerJob(t, f, "contact-header", "12025550100@c.us", 44), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	markup := pickerMarkup(t, f.tgCalls[0])
	if len(markup.InlineKeyboard) != 2 || markup.InlineKeyboard[0][0].Text != "Create bot" || len(markup.InlineKeyboard[0]) != 3 || len(markup.InlineKeyboard[1]) != 3 {
		t.Fatal(markup)
	}
}
func TestGroupControlStoreFencesStaleAndCrossAccountClaims(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "group@g.us", -100, 44)
	m := pickerMessage(13070, -100, 44, "/group")
	if !admitPicker(t, f, "master", models.Update{ID: 3070, Message: m}) {
		t.Fatal("not admitted")
	}
	j := claim(t, f.store)
	other := j
	other.AccountID = "other"
	c := control.GroupSettings{OwnerID: 123, Management: -100, CommandTTL: 60, ControlTTL: 600}
	if _, err := f.store.PrepareGroupControl(t.Context(), other, "bifrost_bot", c); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
	expire(t, f.store, j)
	if _, err := f.store.PrepareGroupControl(t.Context(), j, "bifrost_bot", c); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
}
func TestGroupControlReopensDatabaseWithoutDuplicatingSelectorRequest(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "group@g.us", -100, 44)
	parameter := groupButton(t, f, 3080)
	f.failTG = 2
	if r := operatorCommand(t, f, 3081, "master", 123, 0, "/start "+parameter); r.State != delivery.Pending {
		t.Fatal(r)
	}
	reopened, err := Open(t.Context(), f.store.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	f.store.Close()
	f.store = reopened
	replies, err := control.New(reopened, f.replyClients, -100)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := control.NewGroups(reopened, replies, f.mirrorTelegram, control.GroupOptions{OwnerID: 123})
	if err != nil {
		t.Fatal(err)
	}
	dueMirror(t, f, "tg:master:3081")
	var id int64
	if err := reopened.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup='tg:master:3081'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if r := f.run(t, id, "tg", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		var p struct {
			Update models.Update `json:"update"`
		}
		if json.Unmarshal(j.Payload, &p) != nil {
			return delivery.ErrInvalid
		}
		return groups.Handle(ctx, j, e, "master", &p.Update)
	}); r.State != delivery.Done {
		t.Fatal(r)
	}
	var n int
	if err := reopened.pool.QueryRow(t.Context(), "SELECT count(*) FROM mirror_chat_requests").Scan(&n); err != nil || n != 1 || f.tgCalls[1] != f.tgCalls[2] {
		t.Fatal(n, err)
	}
}
