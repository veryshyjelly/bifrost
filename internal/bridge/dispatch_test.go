package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

// These unexpected capabilities panic through the embedded nil interface. A
// command/media/owner guard must decide its path before touching source state.
type noSource struct{ SourceStore }
type noWhatsApp struct{ WhatsApp }
type recordedText struct{ incoming, outgoing, read int }

func (r *recordedText) IncomingText(context.Context, delivery.Job, *delivery.Effects, relay.Incoming) error {
	r.incoming++
	return nil
}
func (r *recordedText) OutgoingText(context.Context, delivery.Job, *delivery.Effects, relay.Outgoing) error {
	r.outgoing++
	return nil
}
func (r *recordedText) MarkRead(context.Context, delivery.Job, *delivery.Effects) error {
	r.read++
	return nil
}

func telegramJob(raw string) delivery.Job {
	var u models.Update
	_ = json.Unmarshal([]byte(raw), &u)
	payload, _ := json.Marshal(map[string]any{"bot": "master", "update": json.RawMessage(raw)})
	return delivery.Job{Event: delivery.Event{AccountID: "account", Kind: "tg", Dedup: "tg:master:" + strconv.FormatInt(u.ID, 10), Payload: payload}}
}
func TestDispatcherRetainsCommandsEditsAndMixedMediaForDedicatedHandlers(t *testing.T) {
	text := &recordedText{}
	other := 0
	d, err := New(noSource{}, text, noWhatsApp{}, Options{AccountID: "account", SessionID: "session", OwnerID: 123, Extensions: Extensions{Telegram: func(_ context.Context, _ delivery.Job, _ *delivery.Effects, bot string, u *models.Update) error {
		if bot != "master" || u.ID != 1 {
			t.Error("native update lost")
		}
		other++
		return nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"update_id":1,"message":{"message_id":1,"from":{"id":123},"chat":{"id":-200,"type":"supergroup"},"text":"/mention"}}`,
		`{"update_id":1,"edited_message":{"message_id":1,"from":{"id":123},"chat":{"id":-200,"type":"supergroup"},"text":"edited"}}`,
		`{"update_id":1,"message":{"message_id":1,"from":{"id":123},"chat":{"id":-200,"type":"supergroup"},"text":"partial","photo":[{"file_id":"fixture"}]}}`,
	} {
		if err := d.Handlers()["tg"](t.Context(), telegramJob(raw), nil); err != nil {
			t.Fatal(err)
		}
	}
	if other != 3 || text.outgoing != 0 {
		t.Fatal("unsupported content was partially forwarded", other, text.outgoing)
	}
	d.c.Extensions.Telegram = nil
	job := telegramJob(`{"update_id":1,"message":{"message_id":1,"from":{"id":123},"chat":{"id":-200,"type":"supergroup"},"text":"/history"}}`)
	if err := d.Handlers()["tg"](t.Context(), job, nil); !errors.Is(err, delivery.ErrUnsupported) {
		t.Fatal("missing command handler was silently acknowledged", err)
	}
}
func TestDispatcherRechecksNativeOwnerAndExactActorKeys(t *testing.T) {
	text := &recordedText{}
	other := 0
	d, err := New(noSource{}, text, noWhatsApp{}, Options{AccountID: "account", SessionID: "session", OwnerID: 123, Extensions: Extensions{Telegram: func(context.Context, delivery.Job, *delivery.Effects, string, *models.Update) error {
		other++
		return nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"update_id":1,"message":{"message_id":1,"from":{"id":456},"chat":{"id":-200},"text":"hello"}}`,
		`{"update_id":1,"message":{"message_id":1,"from":{"ID":123},"chat":{"id":-200},"text":"hello"}}`,
		`{"update_id":1,"callback_query":{"id":"fixture","from":{"id":456},"data":"owner-control"}}`,
		`{"update_id":1,"message":{"message_id":1,"from":{"id":123,"is_bot":true},"chat":{"id":-200},"text":"hello"}}`,
	} {
		if err := d.Handlers()["tg"](t.Context(), telegramJob(raw), nil); err != nil {
			t.Fatal(err)
		}
	}
	if other != 0 || text.outgoing != 0 {
		t.Fatal("unauthorized update reached a handler")
	}
}
func TestWhatsAppMediaCannotReachTextRelayOrLoseItsOriginalEnvelope(t *testing.T) {
	text := &recordedText{}
	other := 0
	d, err := New(noSource{}, text, noWhatsApp{}, Options{AccountID: "account", SessionID: "session", OwnerID: 123, Extensions: Extensions{WhatsApp: map[sdk.WebhookEvent]delivery.Handler{sdk.EventMessageReceived: func(_ context.Context, j delivery.Job, _ *delivery.Effects) error {
		other++
		if !json.Valid(j.Payload) {
			t.Error("original envelope lost")
		}
		return nil
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	job := delivery.Job{Event: delivery.Event{AccountID: "account", Kind: "wa", Dedup: "ow:fixture", Payload: []byte(`{"event":"message.received","sessionId":"session","accountId":"account","idempotencyKey":"fixture","data":{"id":"fixture","chatId":"123@g.us","type":"image","body":"caption"}}`)}}
	if err := d.Handlers()["wa"](t.Context(), job, nil); err != nil {
		t.Fatal(err)
	}
	if other != 1 || text.incoming != 0 {
		t.Fatal("caption was relayed without its media")
	}
	job.Payload = []byte(`{"event":"message.received","sessionId":"foreign","accountId":"account","idempotencyKey":"fixture","data":{"type":"image"}}`)
	if err := d.Handlers()["wa"](t.Context(), job, nil); !errors.Is(err, delivery.ErrInvalid) || other != 1 {
		t.Fatal("foreign session reached a handler", err)
	}
}
