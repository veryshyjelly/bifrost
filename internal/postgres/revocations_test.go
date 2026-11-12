package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/bridge"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/revocations"
)

type revocationFixture struct {
	f *changeFixture
	s *revocations.Service
	c revocations.Settings
}

func newRevocationFixture(t *testing.T) *revocationFixture {
	t.Helper()
	f := newChangeFixture(t)
	replies, err := control.New(f.n.f.store, func(_ context.Context, _ string, id string) (control.Telegram, error) { return f.n.tgClients[id], nil }, -100)
	if err != nil {
		t.Fatal(err)
	}
	f.replies = replies
	c := revocations.Settings{Session: "fixture-session", OwnerID: 123, ControlTTL: 600, NoticeTTL: 90}
	service, err := revocations.New(f.n.f.store, func(_ context.Context, _ string, id string) (revocations.Telegram, error) {
		return f.n.tgClients[id], nil
	}, f.replies, c)
	if err != nil {
		t.Fatal(err)
	}
	options := f.n.f.dispatchOptions
	options.Extensions.WhatsApp = map[sdk.WebhookEvent]delivery.Handler{sdk.EventMessageReceived: f.n.native.Incoming, sdk.EventMessageSent: f.n.native.Incoming, sdk.EventMessageEdited: f.edits.WhatsApp, sdk.EventMessageRevoked: service.Incoming}
	options.Extensions.Telegram = func(ctx context.Context, j delivery.Job, e *delivery.Effects, id string, u *models.Update) error {
		for _, h := range []bridge.TelegramHandler{service.Callback, f.edits.Telegram, f.n.outbound.Handle} {
			if err := h(ctx, j, e, id, u); !errors.Is(err, delivery.ErrUnsupported) {
				return err
			}
		}
		return delivery.ErrUnsupported
	}
	if options.Extensions.ControlJobs == nil {
		options.Extensions.ControlJobs = map[string]delivery.Handler{}
	}
	options.Extensions.ControlJobs["control-revoke"] = service.Process
	options.Extensions.ControlJobs["control-notice"] = f.replies.Notice
	f.n.f.dispatcher, err = bridge.New(f.n.f.store, f.n.f.service, f.n.f.whatsapp, options)
	if err != nil {
		t.Fatal(err)
	}
	return &revocationFixture{f: f, s: service, c: c}
}
func (f *revocationFixture) revoke(t *testing.T, key, message string) delivery.Report {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"event": "message.revoked", "sessionId": "fixture-session", "idempotencyKey": key, "data": map[string]any{"chatId": "peer@g.us", "revokedId": message}})
	if !fixtureIngress(t, f.f.n.f)("openwa", raw) {
		t.Fatal("revocation not admitted")
	}
	return f.f.n.f.dispatch(t, "ow:"+key, "wa")
}
func (f *revocationFixture) drain(t *testing.T) {
	t.Helper()
	for i := 0; i < 20; i++ {
		var id int64
		err := f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='control-revoke' AND state='pending' AND due<=clock_timestamp() ORDER BY id LIMIT 1").Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if r := f.f.n.f.run(t, id, "control-revoke", f.s.Process); r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	t.Fatal("revocation work failed to converge")
}
func (f *revocationFixture) task(t *testing.T, chat int64) revocations.Task {
	t.Helper()
	var raw []byte
	if err := f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT payload FROM revocation_tasks WHERE (payload->'Primary'->'Target'->>'ChatID')::bigint=$1", chat).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var result revocations.Task
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func (f *revocationFixture) callback(t *testing.T, id int64, task revocations.Task, operation string, message int, thread int64) delivery.Report {
	t.Helper()
	u := models.Update{ID: id, CallbackQuery: &models.CallbackQuery{ID: fmt.Sprint(id), From: models.User{ID: 123}, Data: "rev-" + operation + ":" + task.ID, Message: models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeMessage, Message: pickerMessage(message, task.Primary.Target.ChatID, thread, "")}}}
	if !admitPicker(t, f.f.n.f, task.NoticeBot, u) {
		t.Fatal("choice not admitted")
	}
	return f.f.n.f.dispatch(t, "tg:"+task.NoticeBot+":"+strconv.FormatInt(id, 10), "tg")
}

func TestRevocationOtherAuthorDeletesAllPartsWithoutReplacement(t *testing.T) {
	f := newRevocationFixture(t)
	if r := f.f.n.incoming(t, "source-image", strings.Repeat("Caption", 200)); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.revoke(t, "revoke-other", "source-image"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.drain(t)
	task := f.task(t, -100)
	if task.Status != "deleted" || task.Own || task.Cursor != 2 || task.Blocked != 0 {
		t.Fatal(task)
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	if len(f.f.n.calls) != 4 || f.f.n.calls[2].Method != "deleteMessages" || f.f.n.calls[3].Method != "deleteMessages" {
		t.Fatal("revocation replaced content", f.f.n.calls)
	}
	var count int
	if err := f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_mappings WHERE deleted").Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
}
func TestRevocationBeforeOriginalPreventsLiveResurrection(t *testing.T) {
	f := newRevocationFixture(t)
	if r := f.revoke(t, "revoke-first", "late-original"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.f.n.incoming(t, "late-original", "Do not resurrect"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	if len(f.f.n.calls) != 0 || f.f.n.reads != 0 {
		t.Fatal("revoked source resurrected", f.f.n.calls, f.f.n.reads)
	}
}
func TestRevocationOwnerKeepAndConflictingRepeatedClickPreserveContent(t *testing.T) {
	f := newRevocationFixture(t)
	f.f.document(t)
	if r := f.revoke(t, "revoke-owner", "out-native"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.drain(t)
	task := f.task(t, -100)
	if !task.Own || !task.Presented || task.Status != "pending" {
		t.Fatal(task)
	}
	if r := f.callback(t, 102, task, "delete", 101, 45); r.State != delivery.Done {
		t.Fatal(r)
	}
	if got := f.task(t, -100); got.Status != "pending" {
		t.Fatal("wrong topic consumed choice", got)
	}
	if r := f.callback(t, 103, task, "keep", 101, 44); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.drain(t)
	if r := f.callback(t, 104, task, "delete", 101, 44); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.revoke(t, "revoke-owner-repeat", "out-native"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.drain(t)
	if task := f.task(t, -100); task.Status != "kept" {
		t.Fatal(task)
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	for _, call := range f.f.n.calls {
		if call.Method == "deleteMessages" {
			t.Fatal("Keep was reversed", call)
		}
	}
	var deleted int
	f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_mappings WHERE deleted").Scan(&deleted)
	if deleted != 0 {
		t.Fatal(deleted)
	}
}
func archiveRevocationCopy(t *testing.T, f *revocationFixture, chat, thread, message int64) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"message_id": message, "date": 1700000000, "chat": map[string]any{"id": chat, "type": "supergroup"}, "message_thread_id": thread})
	if _, err := f.f.n.f.store.pool.Exec(t.Context(), "INSERT INTO message_mappings(account_id,scope,telegram_chat_id,telegram_message_id,thread_id,bot_id,whatsapp_chat,whatsapp_message_id,direction,receipt) VALUES('fixture-account','*',$1,$2,$3,'master','peer@g.us','out-native','out',$4)", chat, message, thread, raw); err != nil {
		t.Fatal(err)
	}
	cleanupRow(t, f.f.n.f.store, "*", "master", chat, thread, message, "chat", 1700000000, 0)
}
func TestRevocationDeleteRemovesOnlySelectedDestination(t *testing.T) {
	f := newRevocationFixture(t)
	f.f.document(t)
	archiveRevocationCopy(t, f, -100, 44, 102)
	archiveRevocationCopy(t, f, -200, 0, 301)
	if r := f.revoke(t, "revoke-destinations", "out-native"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.drain(t)
	task := f.task(t, -100)
	if r := f.callback(t, 102, task, "delete", 101, 44); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.drain(t)
	if task := f.task(t, -100); task.Status != "deleted" || task.Cursor != 2 {
		t.Fatal(task)
	}
	if task := f.task(t, -200); task.Status != "pending" {
		t.Fatal("other destination choice changed", task)
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	var deleted []int
	for _, call := range f.f.n.calls {
		if call.Method == "deleteMessages" {
			if call.Chat != -100 {
				t.Fatal("other destination deleted", call)
			}
			deleted = append(deleted, call.DeleteIDs...)
		}
	}
	if len(deleted) != 2 || deleted[0] != 101 || deleted[1] != 102 {
		t.Fatal(deleted)
	}
}
func TestRevocationReplyFallbackExpiresOnlyPrompt(t *testing.T) {
	f := newRevocationFixture(t)
	f.f.document(t)
	f.f.n.failMethod = "editMessageReplyMarkup"
	f.f.n.failCode = 400
	if r := f.revoke(t, "revoke-reply", "out-native"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.drain(t)
	task := f.task(t, -100)
	if task.Mode != "reply" || task.PromptMessageID <= 0 || !task.Presented {
		t.Fatal(task)
	}
	if r := f.callback(t, 102, task, "delete", 101, 44); r.State != delivery.Done {
		t.Fatal(r)
	}
	if got := f.task(t, -100); got.Status != "pending" {
		t.Fatal("original message accepted reply-mode choice", got)
	}
	if _, err := f.f.n.f.store.pool.Exec(t.Context(), "UPDATE revocation_tasks SET payload=jsonb_set(payload,'{ExpiresAt}','1')"); err != nil {
		t.Fatal(err)
	}
	f.f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE kind='control-revoke' AND payload->>'Stage'='expire'")
	f.drain(t)
	if got := f.task(t, -100); got.Status != "kept" {
		t.Fatal(got)
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	for _, call := range f.f.n.calls {
		if call.Method == "deleteMessages" {
			if len(call.DeleteIDs) != 1 || int64(call.DeleteIDs[0]) != task.PromptMessageID {
				t.Fatal("expiry removed content", call)
			}
		}
	}
	var deleted bool
	if err := f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT deleted FROM message_mappings WHERE telegram_message_id=101").Scan(&deleted); err != nil || deleted {
		t.Fatal("expiry changed original mapping", deleted, err)
	}
}
func TestRevocationProviderRefusalPreservesContentAndQueuesPartialNotice(t *testing.T) {
	f := newRevocationFixture(t)
	if r := f.f.n.incoming(t, "source-image", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.failMethod = "deleteMessages"
	f.f.n.failCode = 403
	if r := f.revoke(t, "revoke-refused", "source-image"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.drain(t)
	if task := f.task(t, -100); task.Status != "partial" || task.Blocked != 1 {
		t.Fatal(task)
	}
	var removed bool
	if err := f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT deleted FROM message_mappings").Scan(&removed); err != nil || removed {
		t.Fatal(removed, err)
	}
	var notices int
	f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='control-notice'").Scan(&notices)
	if notices != 1 {
		t.Fatal(notices)
	}
}
func TestRevocationDeleteCrashRepeatsFrozenCopyWithoutUncertainSend(t *testing.T) {
	f := newRevocationFixture(t)
	if r := f.f.n.incoming(t, "source-image", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.revoke(t, "revoke-crash", "source-image"); r.State != delivery.Done {
		t.Fatal(r)
	}
	j := claim(t, f.f.n.f.store)
	task, active, err := f.f.n.f.store.RevocationStep(t.Context(), j, f.c)
	if err != nil || !active {
		t.Fatal(task, active, err)
	}
	copy := task.Copies[0]
	if err := f.f.n.tgClients["master"].DeleteMessages(t.Context(), &bot.DeleteMessagesParams{ChatID: copy.Target.ChatID, MessageIDs: []int{int(copy.MessageID)}}); err != nil {
		t.Fatal(err)
	}
	f.f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", j.ID)
	if count, err := f.f.n.f.store.RecoverExpired(t.Context()); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	f.drain(t)
	if task := f.task(t, -100); task.Status != "deleted" {
		t.Fatal(task)
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	if len(f.f.n.calls) != 3 || f.f.n.calls[1].Method != "deleteMessages" || f.f.n.calls[2].Method != "deleteMessages" || f.f.n.calls[1].DeleteIDs[0] != f.f.n.calls[2].DeleteIDs[0] {
		t.Fatal(f.f.n.calls)
	}
}

func TestRevocationPrivateChoiceRejectsDifferentBotWithSameMessageID(t *testing.T) {
	f := newRevocationFixture(t)
	f.f.n.f.bind(t, "200", "12025550123@c.us")
	f.f.n.f.bind(t, "201", "12025550124@c.us")
	m := pickerMessage(101, 123, 0, "")
	m.Document = &models.Document{FileID: "fixture-file", FileUniqueID: "unique", FileName: "f.png", MimeType: "image/png"}
	if !admitPicker(t, f.f.n.f, "200", models.Update{ID: 101, Message: m}) {
		t.Fatal("private source not admitted")
	}
	if r := f.f.n.f.dispatch(t, "tg:200:101", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	raw, _ := json.Marshal(map[string]any{"event": "message.revoked", "sessionId": "fixture-session", "idempotencyKey": "private-revoke", "data": map[string]any{"chatId": "12025550123@c.us", "revokedId": "out-native"}})
	if !fixtureIngress(t, f.f.n.f)("openwa", raw) {
		t.Fatal("revocation not admitted")
	}
	if r := f.f.n.f.dispatch(t, "ow:private-revoke", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.drain(t)
	task := f.task(t, 123)
	spoof := task
	spoof.NoticeBot = "201"
	if r := f.callback(t, 102, spoof, "delete", 101, 0); r.State != delivery.Done {
		t.Fatal(r)
	}
	if got := f.task(t, 123); got.Status != "pending" {
		t.Fatal("private ID crossed bot scope", got)
	}
	if r := f.callback(t, 103, task, "keep", 101, 0); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.drain(t)
	if got := f.task(t, 123); got.Status != "kept" {
		t.Fatal(got)
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	for _, call := range f.f.n.calls {
		if call.Method == "deleteMessages" {
			t.Fatal("private scope check deleted content", call)
		}
	}
}

func TestRevocationRefusedPromptDoesNotReplaceMissingContent(t *testing.T) {
	f := newRevocationFixture(t)
	f.f.document(t)
	f.f.n.failMethod = "editMessageReplyMarkup"
	f.f.n.failCode = 400
	if r := f.revoke(t, "revoke-prompt-gone", "out-native"); r.State != delivery.Done {
		t.Fatal(r)
	}
	var id int64
	f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='control-revoke' AND payload->>'Stage'='present'").Scan(&id)
	// Freeze the inline refusal, then exercise the missing-parent prompt refusal
	// in the same production control send path with an explicit provider fixture.
	if r := f.f.n.f.run(t, id, "control-revoke", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		if err := f.f.n.f.store.UseRevocationReply(ctx, j); err != nil {
			return err
		}
		f.f.n.mu.Lock()
		f.f.n.failMethod = "sendMessage"
		f.f.n.failDescription = "Bad Request: reply message not found"
		f.f.n.mu.Unlock()
		return f.s.Process(ctx, j, e)
	}); r.State != delivery.Done {
		t.Fatal(r)
	}
	task := f.task(t, -100)
	if task.Status != "gone" || task.Presented {
		t.Fatal(task)
	}
	var deleted int
	f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_mappings WHERE deleted").Scan(&deleted)
	if deleted != 0 {
		t.Fatal("missing prompt changed content", deleted)
	}
	var unknown int
	f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM effects WHERE state IN ('pending','sending','uncertain')").Scan(&unknown)
	if unknown != 0 {
		t.Fatal("known prompt refusal was not closed", unknown)
	}
}

func TestRevocationDeniedPromptCleanupDoesNotClaimItWasDeleted(t *testing.T) {
	f := newRevocationFixture(t)
	f.f.document(t)
	f.f.n.failMethod = "editMessageReplyMarkup"
	f.f.n.failCode = 400
	if r := f.revoke(t, "cleanup-denied", "out-native"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.drain(t)
	task := f.task(t, -100)
	if task.PromptMessageID <= 0 {
		t.Fatal(task)
	}
	if r := f.callback(t, 102, task, "keep", int(task.PromptMessageID), 44); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.mu.Lock()
	f.f.n.failMethod = "deleteMessages"
	f.f.n.failCode = 403
	f.f.n.mu.Unlock()
	f.drain(t)
	if task := f.task(t, -100); task.Status != "kept" {
		t.Fatal(task)
	}
	var deleted bool
	if err := f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT deleted FROM telegram_observations WHERE message_id=$1", task.PromptMessageID).Scan(&deleted); err != nil || deleted {
		t.Fatal("denied cleanup was falsely recorded", deleted, err)
	}
}
