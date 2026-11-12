package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/bridge"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/sourceactions"
	"strconv"
	"testing"
)

type sourceActionFixture struct {
	n *nativeFixture
	s *sourceactions.Service
}

func newSourceActionFixture(t *testing.T) *sourceActionFixture {
	t.Helper()
	f := newRevocationFixture(t)
	service, err := sourceactions.New(f.f.n.f.store, f.f.n.waClient, f.f.replies, sourceactions.Settings{Session: "fixture-session", OwnerID: 123, Management: -100, CommandTTL: 30, NoticeTTL: 90})
	if err != nil {
		t.Fatal(err)
	}
	options := f.f.n.f.dispatchOptions
	options.Extensions.WhatsApp = map[sdk.WebhookEvent]delivery.Handler{sdk.EventMessageReceived: f.f.n.native.Incoming, sdk.EventMessageSent: f.f.n.native.Incoming, sdk.EventMessageRevoked: f.s.Incoming, sdk.EventMessageEdited: f.f.edits.WhatsApp}
	options.Extensions.Telegram = func(ctx context.Context, j delivery.Job, e *delivery.Effects, id string, u *models.Update) error {
		for _, h := range []bridge.TelegramHandler{service.Handle, f.s.Callback, f.f.edits.Telegram, f.f.n.outbound.Handle} {
			if err := h(ctx, j, e, id, u); !errors.Is(err, delivery.ErrUnsupported) {
				return err
			}
		}
		return delivery.ErrUnsupported
	}
	if options.Extensions.ControlJobs == nil {
		options.Extensions.ControlJobs = map[string]delivery.Handler{}
	}
	options.Extensions.ControlJobs["control-source-action"] = service.Run
	options.Extensions.ControlJobs["control-revoke"] = f.s.Process
	options.Extensions.ControlJobs["control-notice"] = f.f.replies.Notice
	options.Extensions.Management["operator_action"] = func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		if err := service.Manage(ctx, j, e); !errors.Is(err, delivery.ErrUnsupported) {
			return err
		}
		return f.f.n.f.operator.Manage(ctx, j, e)
	}
	f.f.n.f.dispatcher, err = bridge.New(f.f.n.f.store, f.f.n.f.service, f.f.n.f.whatsapp, options)
	if err != nil {
		t.Fatal(err)
	}
	return &sourceActionFixture{n: f.f.n, s: service}
}
func (f *sourceActionFixture) command(t *testing.T, id int64, text string, reply bool, edited bool) delivery.Report {
	t.Helper()
	m := pickerMessage(int(id)+100, -100, 44, text)
	if reply {
		m.ReplyToMessage = pickerMessage(2001, -100, 44, "")
	}
	u := models.Update{ID: id}
	if edited {
		u.EditedMessage = m
	} else {
		u.Message = m
	}
	if !admitPicker(t, f.n.f, "master", u) {
		t.Fatal("command not admitted")
	}
	return f.n.f.dispatch(t, "tg:master:"+strconv.FormatInt(id, 10), "tg")
}
func (f *sourceActionFixture) pending(t *testing.T) int64 {
	t.Helper()
	var id int64
	if err := f.n.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='control-source-action' AND state='pending' ORDER BY id LIMIT 1").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func TestSourceActionsRequireMappedReplyAndIgnoreEditedCommands(t *testing.T) {
	f := newSourceActionFixture(t)
	if r := f.command(t, 101, "/delete", false, false); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.command(t, 102, "/react 👍", false, true); r.State != delivery.Done {
		t.Fatal(r)
	}
	var count int
	f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM source_action_jobs").Scan(&count)
	if count != 0 {
		t.Fatal(count)
	}
	f.n.mu.Lock()
	defer f.n.mu.Unlock()
	if len(f.n.sends) != 0 {
		t.Fatal(f.n.sends)
	}
}
func TestSourceActionsNativeReactionAndReplayKeepOriginalSource(t *testing.T) {
	f := newSourceActionFixture(t)
	if r := f.n.incoming(t, "source-image", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.command(t, 101, "/react 👍 ignored-extra", true, false); r.State != delivery.Done {
		t.Fatal(r)
	}
	id := f.pending(t)
	if r := f.n.f.run(t, id, "control-source-action", f.s.Run); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET state='pending',due=clock_timestamp() WHERE dedup='tg:master:101'")
	if r := f.n.f.dispatch(t, "tg:master:101", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.mu.Lock()
	defer f.n.mu.Unlock()
	if len(f.n.sends) != 1 || f.n.sends[0].Method != "react" || string(f.n.sends[0].Request["emoji"]) != `"👍"` || string(f.n.sends[0].Request["messageId"]) != `"source-image"` {
		t.Fatal(f.n.sends)
	}
	var expiry int64
	if err := f.n.f.store.pool.QueryRow(t.Context(), "SELECT expires_at FROM telegram_observations WHERE message_id=201 AND kind='command'").Scan(&expiry); err != nil || expiry <= 0 {
		t.Fatal(expiry, err)
	}
}
func TestSourceActionsDeleteAcknowledgementDoesNotInventSourceRevocation(t *testing.T) {
	f := newSourceActionFixture(t)
	if r := f.n.incoming(t, "source-image", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.command(t, 101, "/delete", true, false); r.State != delivery.Done {
		t.Fatal(r)
	}
	id := f.pending(t)
	if r := f.n.f.run(t, id, "control-source-action", f.s.Run); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.mu.Lock()
	if len(f.n.sends) != 1 || f.n.sends[0].Method != "delete" || string(f.n.sends[0].Request["forEveryone"]) != "true" {
		t.Fatal(f.n.sends)
	}
	for _, call := range f.n.calls {
		if call.Method == "deleteMessages" {
			t.Fatal("gateway ack deleted Telegram", call)
		}
	}
	f.n.mu.Unlock()
	if revoked, err := f.n.f.store.MessageRevoked(t.Context(), "fixture-account", "peer@g.us", "source-image"); err != nil || revoked {
		t.Fatal("gateway ack invented revoke", revoked, err)
	}
}
func TestSourceActionsPermissionRefusalKeepsTelegramContentAndAllowsNextAction(t *testing.T) {
	f := newSourceActionFixture(t)
	if r := f.n.incoming(t, "source-image", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.sendFailure = 403
	f.n.sendFailMethod = "delete"
	if r := f.command(t, 101, "/delete", true, false); r.State != delivery.Done {
		t.Fatal(r)
	}
	id := f.pending(t)
	if r := f.n.f.run(t, id, "control-source-action", f.s.Run); r.State != delivery.Done {
		t.Fatal(r)
	}
	var notices, deleted int
	f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='control-notice'").Scan(&notices)
	f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_mappings WHERE deleted").Scan(&deleted)
	if notices != 1 || deleted != 0 {
		t.Fatal(notices, deleted)
	}
	f.n.mu.Lock()
	f.n.sendFailure = 0
	f.n.mu.Unlock()
	if r := f.command(t, 102, "/react", true, false); r.State != delivery.Done {
		t.Fatal(r)
	}
	id = f.pending(t)
	if r := f.n.f.run(t, id, "control-source-action", f.s.Run); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.mu.Lock()
	defer f.n.mu.Unlock()
	if len(f.n.sends) != 2 || string(f.n.sends[1].Request["emoji"]) != `""` {
		t.Fatal("empty /react did not clear explicitly", f.n.sends)
	}
}
func TestSourceActionsCrashAfterSetAcceptanceReusesFrozenRequest(t *testing.T) {
	f := newSourceActionFixture(t)
	if r := f.n.incoming(t, "source-image", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.command(t, 101, "/react 👍", true, false); r.State != delivery.Done {
		t.Fatal(r)
	}
	id := f.pending(t)
	j := claim(t, f.n.f.store)
	if j.ID != id {
		t.Fatal(j, id)
	}
	action, err := f.n.f.store.SourceAction(t.Context(), j)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.n.waClient.React(t.Context(), action.Session, sdk.ReactMessageRequest{ChatID: action.ChatID, MessageID: action.MessageID, Emoji: action.Emoji}); err != nil {
		t.Fatal(err)
	}
	f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", j.ID)
	if count, err := f.n.f.store.RecoverExpired(t.Context()); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if r := f.n.f.run(t, id, "control-source-action", f.s.Run); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.mu.Lock()
	defer f.n.mu.Unlock()
	if len(f.n.sends) != 2 || string(f.n.sends[0].Request["emoji"]) != string(f.n.sends[1].Request["emoji"]) {
		t.Fatal(f.n.sends)
	}
}

func TestSourceActionsAuthenticatedManagementReplayDoesNotRequeue(t *testing.T) {
	f := newSourceActionFixture(t)
	raw, _ := json.Marshal(map[string]any{"id": "source-management", "type": "operator_action", "data": map[string]any{"operation": "react", "chatId": "peer@g.us", "message": "source-message", "emoji": "👍"}, "at": 1794450000000})
	id := f.n.f.enqueue(t, "management", "management:source-management", "management:source-management", "")
	if _, err := f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET payload=$2 WHERE id=$1", id, raw); err != nil {
		t.Fatal(err)
	}
	if r := f.n.f.run(t, id, "management", f.s.Manage); r.State != delivery.Done {
		t.Fatal(r)
	}
	action := f.pending(t)
	if r := f.n.f.run(t, action, "control-source-action", f.s.Run); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET state='pending',due=clock_timestamp() WHERE id=$1", id)
	if r := f.n.f.run(t, id, "management", f.s.Manage); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.mu.Lock()
	defer f.n.mu.Unlock()
	if len(f.n.sends) != 1 {
		t.Fatal("management replay repeated mutation", f.n.sends)
	}
}
