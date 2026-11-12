package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/bridge"
	"github.com/veryshyjelly/bifrost/internal/changes"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

type changeFixture struct {
	n       *nativeFixture
	edits   *changes.Edits
	replies *control.Service
}

func newChangeFixture(t *testing.T) *changeFixture {
	t.Helper()
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	replies, err := control.New(n.f.store, n.f.replyClients, -100)
	if err != nil {
		t.Fatal(err)
	}
	service, err := changes.NewEdits(n.f.store, n.waClient, func(_ context.Context, _ string, id string) (changes.Telegram, error) { return n.tgClients[id], nil }, n.f.mentionService, changes.Settings{Session: "fixture-session", OwnerID: 123, Management: -100, NoticeTTL: 60})
	if err != nil {
		t.Fatal(err)
	}
	options := n.f.dispatchOptions
	options.Extensions.WhatsApp = map[sdk.WebhookEvent]delivery.Handler{sdk.EventMessageReceived: n.native.Incoming, sdk.EventMessageSent: n.native.Incoming, sdk.EventMessageEdited: service.WhatsApp}
	options.Extensions.Telegram = func(ctx context.Context, j delivery.Job, e *delivery.Effects, id string, u *models.Update) error {
		if err := service.Telegram(ctx, j, e, id, u); !errors.Is(err, delivery.ErrUnsupported) {
			return err
		}
		return n.outbound.Handle(ctx, j, e, id, u)
	}
	if options.Extensions.ControlJobs == nil {
		options.Extensions.ControlJobs = map[string]delivery.Handler{}
	}
	options.Extensions.ControlJobs["control-notice"] = replies.Notice
	n.f.dispatcher, err = bridge.New(n.f.store, n.f.service, n.f.whatsapp, options)
	if err != nil {
		t.Fatal(err)
	}
	return &changeFixture{n: n, edits: service, replies: replies}
}
func (f *changeFixture) update(t *testing.T, id int64, m *models.Message, edited bool) delivery.Report {
	t.Helper()
	u := models.Update{ID: id}
	if edited {
		u.EditedMessage = m
	} else {
		u.Message = m
	}
	if !admitPicker(t, f.n.f, "master", u) {
		t.Fatal("update not admitted")
	}
	return f.n.f.dispatch(t, "tg:master:"+stringID(id), "tg")
}
func stringID(id int64) string { return strconv.FormatInt(id, 10) }
func (f *changeFixture) sourceEdit(t *testing.T, key, id, body string) delivery.Report {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"event": "message.edited", "sessionId": "fixture-session", "idempotencyKey": key, "data": map[string]any{"chatId": "peer@g.us", "messageId": id, "body": body}})
	if !fixtureIngress(t, f.n.f)("openwa", raw) {
		t.Fatal("source edit not admitted")
	}
	return f.n.f.dispatch(t, "ow:"+key, "wa")
}
func (f *changeFixture) document(t *testing.T) *models.Message {
	t.Helper()
	m := pickerMessage(101, -100, 44, "")
	m.Document = &models.Document{FileID: "fixture-file", FileUniqueID: "unique", FileName: "image.png", MimeType: "image/png"}
	m.Caption = "Caption"
	if r := f.update(t, 101, m, false); r.State != delivery.Done {
		t.Fatal(r)
	}
	return m
}

func TestMappedEditCaptionClearingUsesProviderAndRefusalNoticeIsIndependent(t *testing.T) {
	for _, refused := range []bool{false, true} {
		t.Run(stringID(int64(boolInt(refused))), func(t *testing.T) {
			f := newChangeFixture(t)
			m := f.document(t)
			m.Caption = ""
			m.EditDate = 1700000001
			if refused {
				f.n.sendFailure = 400
				f.n.sendFailMethod = "edit"
			}
			if r := f.update(t, 102, m, true); r.State != delivery.Done {
				t.Fatal(r)
			}
			f.n.mu.Lock()
			if len(f.n.sends) != 2 || f.n.sends[1].Method != "edit" || string(f.n.sends[1].Request["body"]) != `""` {
				t.Fatal("empty caption was not attempted exactly", f.n.sends)
			}
			f.n.sendFailure = 0
			f.n.mu.Unlock()
			var notices int
			f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='control-notice' AND state='pending'").Scan(&notices)
			if notices != boolInt(refused) {
				t.Fatal(notices)
			}
			if refused {
				next := pickerMessage(102, -100, 44, "Next message")
				if r := f.update(t, 103, next, false); r.State != delivery.Done {
					t.Fatal("refused edit blocked later send", r)
				}
				var id int64
				f.n.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='control-notice'").Scan(&id)
				if r := f.n.f.run(t, id, "control-notice", f.replies.Notice); r.State != delivery.Done {
					t.Fatal(r)
				}
			} else {
				if r := f.sourceEdit(t, "clear-echo", "out-native", ""); r.State != delivery.Done {
					t.Fatal(r)
				}
				f.n.mu.Lock()
				defer f.n.mu.Unlock()
				if len(f.n.calls) != 0 {
					t.Fatal("confirmed edit echo forwarded", f.n.calls)
				}
			}
		})
	}
}
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestMappedEditIncomingPreservesSenderAndDoesNotRecreateClearedCopy(t *testing.T) {
	f := newChangeFixture(t)
	if r := f.n.incoming(t, "in-image", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.mu.Lock()
	prefix := strings.TrimSuffix(f.n.calls[0].Text, "Caption")
	f.n.mu.Unlock()
	if _, err := f.n.f.store.pool.Exec(t.Context(), "UPDATE contact_profiles SET name='Renamed' WHERE account_id='fixture-account'"); err != nil {
		t.Fatal(err)
	}
	if r := f.sourceEdit(t, "edit-image", "in-image", "Changed <literal>"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.mu.Lock()
	if len(f.n.calls) != 2 || f.n.calls[1].Method != "editMessageCaption" || f.n.calls[1].Text != prefix+"Changed <literal>" {
		t.Fatal("frozen caption sender changed", f.n.calls)
	}
	f.n.mu.Unlock()
	f.n.f.store.pool.Exec(t.Context(), "UPDATE telegram_observations SET deleted=true WHERE kind='chat'")
	if r := f.sourceEdit(t, "edit-cleared", "in-image", "Do not recreate"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.mu.Lock()
	defer f.n.mu.Unlock()
	if len(f.n.calls) != 2 {
		t.Fatal("cleared destination recreated", f.n.calls)
	}
}

func TestMappedEditBeforeOriginalChangesFrozenIncomingBody(t *testing.T) {
	f := newChangeFixture(t)
	if r := f.sourceEdit(t, "edit-first", "later", "Latest body"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.n.incoming(t, "later", "Old body"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.mu.Lock()
	defer f.n.mu.Unlock()
	if len(f.n.calls) != 1 || !strings.Contains(f.n.calls[0].Text, "Latest body") || strings.Contains(f.n.calls[0].Text, "Old body") {
		t.Fatal(f.n.calls)
	}
}

func TestMappedEditMissingDestinationDoesNotCreateReplacementNotice(t *testing.T) {
	f := newChangeFixture(t)
	if r := f.n.incoming(t, "missing", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.failMethod = "editMessageCaption"
	f.n.failCode = 400
	f.n.failDescription = "Bad Request: message to edit not found"
	if r := f.sourceEdit(t, "edit-missing", "missing", "New"); r.State != delivery.Done {
		t.Fatal(r)
	}
	var notices int
	f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='control-notice'").Scan(&notices)
	if notices != 0 {
		t.Fatal("missing copy replaced", notices)
	}
}

func TestMappedEditRefusalQueuesScopedEditedNoticeAndRetryReusesPlan(t *testing.T) {
	f := newChangeFixture(t)
	if r := f.n.incoming(t, "retry-edit", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.n.failMethod = "editMessageCaption"
	f.n.failCode = 429
	first := f.sourceEdit(t, "edit-rate", "retry-edit", "New <literal>")
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	f.n.mu.Lock()
	f.n.failCode = 400
	f.n.mu.Unlock()
	f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", first.JobID)
	if r := f.n.f.dispatch(t, "ow:edit-rate", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	var raw []byte
	if err := f.n.f.store.pool.QueryRow(t.Context(), "SELECT payload FROM jobs WHERE kind='control-notice'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var n control.Notice
	if json.Unmarshal(raw, &n) != nil || n.Target.BotID != "master" || n.Target.ChatID != -100 || n.Target.ThreadID != 44 || n.ReplyTo != 2001 || !strings.Contains(n.Body.Text, "New <literal>") {
		t.Fatal(n)
	}
	f.n.mu.Lock()
	defer f.n.mu.Unlock()
	if len(f.n.calls) != 3 || f.n.calls[1].Text != f.n.calls[2].Text {
		t.Fatal("known retry rerendered", f.n.calls)
	}
}
