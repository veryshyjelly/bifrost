// Package relay freezes text delivery plans, invokes providers through the
// effect ledger, and records quote/echo mappings only after confirmed outcomes.
package relay

import (
	"context"
	"encoding/json"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/attachments"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/mentions"
	"github.com/veryshyjelly/bifrost/internal/presentation"
)

type Telegram interface {
	SendText(context.Context, *bot.SendMessageParams) (*models.Message, error)
}
type TelegramClients func(context.Context, string, string) (Telegram, error)
type Mentions interface {
	Incoming(context.Context, delivery.Job, string, string, []string, int64) (presentation.Value, error)
	Outgoing(context.Context, delivery.Job, string, string, []models.MessageEntity) (mentions.Outgoing, error)
}
type WhatsApp interface {
	SendText(context.Context, string, sdk.SendTextRequest, string) (*sdk.MessageResponse, error)
	MarkRead(context.Context, string, string) error
}

type Incoming struct {
	ChatID, MessageID, SenderID, LinkSenderID, Name, ConversationName, Body, QuotedMessageID string
	Own                                                                                      bool
	MentionedIDs                                                                             []string
}
type Outgoing struct {
	BotID   string
	Message *models.Message
	Edited  bool
}

type TelegramRequest struct {
	BotID    string                 `json:"bot"`
	ChatID   int64                  `json:"chat_id"`
	ThreadID int64                  `json:"message_thread_id"`
	Text     string                 `json:"text"`
	Entities []models.MessageEntity `json:"entities"`
	ReplyTo  int64                  `json:"reply_to"`
}
type WhatsAppRequest struct {
	ChatID          string                   `json:"chatId"`
	Text            string                   `json:"text"`
	QuotedMessageID string                   `json:"quotedMessageId,omitempty"`
	Mentions        []string                 `json:"mentions,omitempty"`
	Kind            string                   `json:"kind,omitempty"`
	Asset           *attachments.Asset       `json:"asset,omitempty"`
	PTT             bool                     `json:"ptt,omitempty"`
	Location        *sdk.SendLocationRequest `json:"location,omitempty"`
	Contact         *sdk.SendContactRequest  `json:"contact,omitempty"`
	Poll            *sdk.SendPollRequest     `json:"poll,omitempty"`
}
type Plan struct {
	Key                                     string `json:"key"`
	Direction                               string `json:"direction"`
	WhatsAppChat, WhatsAppMessage           string
	BotID                                   string
	TelegramChat, TelegramMessage, ThreadID int64
	Prefix                                  string
	Route                                   conversation.Route
	Parts                                   []TelegramRequest
	Presentation                            *presentation.Options `json:"Presentation,omitempty"`
	Outgoing                                *WhatsAppRequest
	OutgoingFallback                        *WhatsAppRequest   `json:"OutgoingFallback,omitempty"`
	UseOutgoingFallback                     bool               `json:"UseOutgoingFallback,omitempty"`
	SourceAsset                             *attachments.Asset `json:"SourceAsset,omitempty"`
	Complete                                bool               `json:"-"`
}
type Mapping struct {
	WhatsAppChat, WhatsAppMessage, BotID, Direction string
	TelegramChat, TelegramMessage, ThreadID         int64
	Deleted                                         bool
}
type Echo struct{ Recognized, Pending bool }

type Store interface {
	MessageRevoked(context.Context, string, string, string) (bool, error)
	MessageComplete(context.Context, string, string, string) (bool, error)
	ConversationRoute(context.Context, string, string) (conversation.Route, bool, error)
	RelayPlan(context.Context, string, string) (Plan, bool, error)
	PrepareRelay(context.Context, delivery.Job, Plan) (Plan, error)
	RelayEcho(context.Context, string, string, string) (Echo, error)
	WhatsAppQuote(context.Context, string, string, string, string, int64, int64) (Mapping, bool, error)
	TelegramMapping(context.Context, string, string, int64, int64) (Mapping, bool, error)
	OutgoingRoute(context.Context, delivery.Job, string, int64, int64, int64) (conversation.Route, bool, error)
	RecordRelay(context.Context, delivery.Job, string, int) error
	CompleteRelay(context.Context, delivery.Job, string) error
	CancelRelay(context.Context, delivery.Job, string) error
}

type Routing interface {
	EnsureInbox(context.Context, delivery.Job, *delivery.Effects, string, string) (conversation.Route, error)
	SelectTarget(context.Context, string, conversation.Route, string, conversation.Flags) (conversation.Target, error)
}

// FrozenRequest is shared by execution and transactional receipt reconciliation.
func (p Plan) FrozenRequest(part int) (string, json.RawMessage, error) {
	if p.Direction == "out" {
		if p.Outgoing == nil || part != 0 {
			return "", nil, delivery.ErrInvalid
		}
		b, e := json.Marshal(p.Outgoing)
		if p.UseOutgoingFallback {
			if p.OutgoingFallback == nil {
				return "", nil, delivery.ErrInvalid
			}
			b, e = json.Marshal(p.OutgoingFallback)
			return p.Prefix + ":original", b, e
		}
		return p.Prefix, b, e
	}
	if part < 0 || part >= len(p.Parts) {
		return "", nil, delivery.ErrInvalid
	}
	b, e := json.Marshal(p.Parts[part])
	return p.PartKey(part), b, e
}
