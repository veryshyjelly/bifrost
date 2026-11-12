package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/providers"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

type SourceStore interface {
	NormalizeIncoming(context.Context, delivery.Job) (identity.Source, error)
	SelfIdentity(context.Context, string) (string, bool, error)
	SaveSelfIdentity(context.Context, delivery.Job, string) error
	RelayPlan(context.Context, string, string) (relay.Plan, bool, error)
	RelayEcho(context.Context, string, string, string) (relay.Echo, error)
	ConversationRoute(context.Context, string, string) (conversation.Route, bool, error)
}
type TextRelay interface {
	IncomingText(context.Context, delivery.Job, *delivery.Effects, relay.Incoming) error
	OutgoingText(context.Context, delivery.Job, *delivery.Effects, relay.Outgoing) error
	MarkRead(context.Context, delivery.Job, *delivery.Effects) error
}
type WhatsApp interface {
	Session(context.Context, string) (*sdk.SessionResponse, error)
	Group(context.Context, string, string) (*sdk.GroupInfo, error)
}
type TelegramHandler func(context.Context, delivery.Job, *delivery.Effects, string, *models.Update) error
type MentionRefusalHandler func(context.Context, delivery.Job, *delivery.Effects, string, *models.Message, error) (bool, error)
type Extensions struct {
	// PassiveTopics can consume archive guidance and controls before text relay.
	PassiveTopics TelegramHandler
	// Incoming replaces initial text and attachment delivery while the legacy
	// text-plan resumer retains any already frozen packets and receipts.
	Incoming       delivery.Handler
	WhatsApp       map[sdk.WebhookEvent]delivery.Handler
	Telegram       TelegramHandler
	MentionPicker  TelegramHandler
	OwnerCommands  TelegramHandler
	MentionRefusal MentionRefusalHandler
	Management     map[string]delivery.Handler
	ControlJobs    map[string]delivery.Handler
}
type Options struct {
	AccountID, SessionID string
	OwnerID              int64
	Extensions           Extensions
}
type Dispatcher struct {
	store SourceStore
	text  TextRelay
	wa    WhatsApp
	c     Options
}

func New(store SourceStore, text TextRelay, wa WhatsApp, c Options) (*Dispatcher, error) {
	if store == nil || text == nil || wa == nil || c.AccountID == "" || c.SessionID == "" || c.OwnerID <= 0 {
		return nil, delivery.ErrInvalid
	}
	for kind, h := range c.Extensions.ControlJobs {
		if !strings.HasPrefix(kind, "control-") || h == nil {
			return nil, delivery.ErrInvalid
		}
	}
	return &Dispatcher{store: store, text: text, wa: wa, c: c}, nil
}

// Handlers is explicitly supplied to the fenced worker by its composition root.
// Constructing a dispatcher does not launch a consumer or register callbacks.
func (d *Dispatcher) Handlers() map[string]delivery.Handler {
	handlers := map[string]delivery.Handler{"wa": d.whatsApp, "tg": d.telegram, "management": d.management}
	for kind, h := range d.c.Extensions.ControlJobs {
		handlers[kind] = h
	}
	return handlers
}
func local(err error) error {
	if err == nil || errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, delivery.ErrDeferred) || errors.Is(err, delivery.ErrInvalid) || errors.Is(err, delivery.ErrUnsupported) || errors.Is(err, identity.ErrConflict) {
		return err
	}
	return delivery.ErrLocalRetry
}
func (d *Dispatcher) whatsApp(ctx context.Context, job delivery.Job, e *delivery.Effects) error {
	var hook sdk.WebhookDelivery
	var body struct {
		AccountID string `json:"accountId"`
	}
	if job.Kind != "wa" || job.AccountID != d.c.AccountID || json.Unmarshal(job.Payload, &hook) != nil || json.Unmarshal(job.Payload, &body) != nil || body.AccountID != d.c.AccountID || hook.SessionID != d.c.SessionID || hook.IdempotencyKey == "" || job.Dedup != "ow:"+hook.IdempotencyKey {
		return delivery.ErrInvalid
	}
	if hook.Event != sdk.EventMessageReceived && hook.Event != sdk.EventMessageSent {
		return d.otherWhatsApp(ctx, job, e, hook.Event)
	}
	var native sdk.ChatHistoryMessage
	if json.Unmarshal(hook.Data, &native) != nil {
		return delivery.ErrInvalid
	}
	if native.IsStatusBroadcast || native.Kind == "channel" || native.Kind == "status" || native.Kind == "broadcast" {
		return nil
	}
	if h := d.c.Extensions.Incoming; h != nil {
		return h(ctx, job, e)
	}
	if native.Type != sdk.MsgText || native.Media != nil || native.Location != nil || native.Poll != nil || native.Order != nil || native.Product != nil {
		return d.otherWhatsApp(ctx, job, e, hook.Event)
	}
	source, err := d.store.NormalizeIncoming(ctx, job)
	if err != nil {
		return local(err)
	}
	job.Stream = "wa:" + source.ChatID
	// Existing plans and echoes do not need a new identity/provider lookup.
	_, prepared, err := d.store.RelayPlan(ctx, job.AccountID, relay.IncomingKey(source.ChatID, source.MessageID))
	if err != nil {
		return local(err)
	}
	echo, err := d.store.RelayEcho(ctx, job.AccountID, source.ChatID, source.MessageID)
	if err != nil {
		return local(err)
	}
	if !prepared && !echo.Recognized && !(source.Own && echo.Pending) {
		route, known, err := d.store.ConversationRoute(ctx, job.AccountID, source.ChatID)
		if err != nil {
			return local(err)
		}
		if !known || route.Mode != conversation.Muted && route.Mode != conversation.Ignored {
			if source.Own && source.LinkSenderID == "" {
				id, err := d.self(ctx, job)
				if err != nil {
					return err
				}
				source.LinkSenderID = id
			}
			if strings.HasSuffix(source.ChatID, "@g.us") && !known {
				info, err := d.wa.Group(ctx, d.c.SessionID, source.RawChatID)
				if err != nil {
					return err
				}
				if info == nil || info.ID != source.RawChatID || strings.TrimSpace(info.Name) == "" {
					return providers.InvalidResponse("openwa", false)
				}
				source.ConversationName = info.Name
			}
		}
	}
	return d.text.IncomingText(ctx, job, e, relay.Incoming{ChatID: source.ChatID, MessageID: source.MessageID, SenderID: source.SenderID, LinkSenderID: source.LinkSenderID, Name: source.Name, ConversationName: source.ConversationName, Body: source.Body, QuotedMessageID: source.QuotedMessageID, Own: source.Own, MentionedIDs: source.MentionedIDs})
}
func (d *Dispatcher) otherWhatsApp(ctx context.Context, job delivery.Job, e *delivery.Effects, event sdk.WebhookEvent) error {
	if h := d.c.Extensions.WhatsApp[event]; h != nil {
		return h(ctx, job, e)
	}
	return delivery.ErrUnsupported
}
func (d *Dispatcher) self(ctx context.Context, job delivery.Job) (string, error) {
	if id, found, err := d.store.SelfIdentity(ctx, job.AccountID); err != nil {
		return "", local(err)
	} else if found {
		return id, nil
	}
	response, err := d.wa.Session(ctx, d.c.SessionID)
	if err != nil {
		return "", err
	}
	if response == nil || response.ID != d.c.SessionID {
		return "", providers.InvalidResponse("openwa", false)
	}
	if response.Phone == nil {
		return "", nil
	}
	id, valid := identity.PhoneID(*response.Phone)
	if !valid {
		return "", providers.InvalidResponse("openwa", false)
	}
	return id, local(d.store.SaveSelfIdentity(ctx, job, id))
}

func (d *Dispatcher) telegram(ctx context.Context, job delivery.Job, e *delivery.Effects) error {
	var envelope struct {
		Bot    string          `json:"bot"`
		Update json.RawMessage `json:"update"`
	}
	var update models.Update
	if job.Kind != "tg" || job.AccountID != d.c.AccountID || json.Unmarshal(job.Payload, &envelope) != nil || envelope.Bot == "" || json.Unmarshal(envelope.Update, &update) != nil || job.Dedup != "tg:"+envelope.Bot+":"+strconv.FormatInt(update.ID, 10) {
		return delivery.ErrInvalid
	}
	if !ingress.AuthorizedUpdate(envelope.Update, &update, d.c.OwnerID) {
		return nil
	}
	if h := d.c.Extensions.PassiveTopics; h != nil {
		if err := h(ctx, job, e, envelope.Bot, &update); !errors.Is(err, delivery.ErrUnsupported) {
			return err
		}
	}
	m := update.Message
	if m != nil && m.Text != "" && !hasOtherContent(envelope.Update) && !strings.HasPrefix(strings.TrimFunc(m.Text, func(r rune) bool { return r == 0xfeff || (r != 0x85 && unicode.IsSpace(r)) }), "/") {
		if h := d.c.Extensions.MentionRefusal; h != nil {
			if handled, err := h(ctx, job, e, envelope.Bot, m, nil); handled || err != nil {
				return err
			}
		}
		err := d.text.OutgoingText(ctx, job, e, relay.Outgoing{BotID: envelope.Bot, Message: m})
		if err != nil {
			if h := d.c.Extensions.MentionRefusal; h != nil {
				if handled, noticeErr := h(ctx, job, e, envelope.Bot, m, err); handled {
					return noticeErr
				}
			}
		}
		return err
	}
	if h := d.c.Extensions.MentionPicker; h != nil {
		if err := h(ctx, job, e, envelope.Bot, &update); !errors.Is(err, delivery.ErrUnsupported) {
			return err
		}
	}
	if h := d.c.Extensions.OwnerCommands; h != nil {
		if err := h(ctx, job, e, envelope.Bot, &update); !errors.Is(err, delivery.ErrUnsupported) {
			return err
		}
	}
	if h := d.c.Extensions.Telegram; h != nil {
		return h(ctx, job, e, envelope.Bot, &update)
	}
	return delivery.ErrUnsupported
}
func hasOtherContent(raw json.RawMessage) bool {
	var u struct {
		Message map[string]json.RawMessage `json:"message"`
	}
	if json.Unmarshal(raw, &u) != nil {
		return true
	}
	for _, key := range []string{"caption", "animation", "audio", "document", "paid_media", "photo", "sticker", "story", "video", "video_note", "voice", "contact", "dice", "game", "poll", "venue", "location", "checklist"} {
		if value, ok := u.Message[key]; ok && string(value) != "null" {
			return true
		}
	}
	return false
}
func (d *Dispatcher) management(ctx context.Context, job delivery.Job, e *delivery.Effects) error {
	var event struct {
		Type string `json:"type"`
	}
	if job.Kind != "management" || job.AccountID != d.c.AccountID || json.Unmarshal(job.Payload, &event) != nil || event.Type == "" {
		return delivery.ErrInvalid
	}
	if event.Type == "mark_read" {
		return d.text.MarkRead(ctx, job, e)
	}
	if h := d.c.Extensions.Management[event.Type]; h != nil {
		return h(ctx, job, e)
	}
	return delivery.ErrUnsupported
}
