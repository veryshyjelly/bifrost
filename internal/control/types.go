// Package control journals Telegram owner replies, scoped callback actions and
// idempotent cleanup while preserving conversation archives and delivery effects.
package control

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

// Target scopes a Telegram destination to a sender bot, chat and optional forum thread.
type Target struct {
	BotID            string
	ChatID, ThreadID int64
}
type Context struct {
	Target Target
	Route  conversation.Route
	Known  bool
}

// Action binds a presented callback to its owner, destination, operation and expiry.
type Action struct {
	ID, Kind, BotID, WhatsAppChat, Search string
	OwnerID, ChatID, ThreadID, ExpiresAt  int64
	Page                                  int
	Operation, AccountID, ReturnKey       string `json:",omitempty"`
}

// Packet freezes one owner-facing reply, including entity formatting and optional destination override.
type Packet struct {
	Text           string
	Entities       []models.MessageEntity
	Markup         *models.InlineKeyboardMarkup
	Keyboard       *models.ReplyKeyboardMarkup `json:",omitempty"`
	RemoveKeyboard bool                        `json:",omitempty"`
	ReplyTo        int64                       `json:",omitempty"`
	StrictReply    bool                        `json:",omitempty"`
	Target         *Target                     `json:",omitempty"`
}
type Command struct {
	MessageID, Date int64
	TTLSeconds      int
}

// Plan persists replies and associated control intent before any Telegram effects execute.
// Packet keys remain stable when a partially completed reply resumes.
type Plan struct {
	Key             string
	Prefix          string
	Target          Target
	Packets         []Packet
	Answer          *bot.AnswerCallbackQueryParams
	Actions         []Action
	Scope           *Context
	Command         *Command
	Clear           *ClearRequest
	Operator        *OperatorRequest
	Start           *StartRequest
	Mirror          *MirrorRequest
	Header          bool `json:",omitempty"`
	ReplyTTLSeconds int
	Complete        bool `json:"-"`
}

// PacketTarget applies a per-packet destination override when one was frozen in the plan.
func (p Plan) PacketTarget(part int) Target {
	if p.Packets[part].Target != nil {
		return *p.Packets[part].Target
	}
	return p.Target
}

func (p Plan) PacketKey(part int) string {
	prefix := p.Prefix
	if prefix == "" {
		prefix = p.Key
	}
	return prefix + ":text:" + strconv.Itoa(part)
}

// Keep integer serialization in one place for native and baseline effect keys.
func (p Plan) FrozenPacket(part int) (string, json.RawMessage, error) {
	if part < 0 || part >= len(p.Packets) {
		return "", nil, delivery.ErrInvalid
	}
	b, err := json.Marshal(struct {
		Target Target
		Packet Packet
	}{p.PacketTarget(part), p.Packets[part]})
	return p.PacketKey(part), b, err
}

// Store admits and resumes owner reply plans independently of provider send attempts.
type Store interface {
	ControlContext(context.Context, string, string, *models.Message, int64) (Context, error)
	ControlPlan(context.Context, string, string) (Plan, bool, error)
	PrepareControl(context.Context, delivery.Job, Plan) (Plan, error)
	ControlAction(context.Context, string, string) (Action, bool, error)
	ControlWasCommand(context.Context, string, string, int64, int64) (bool, error)
	RecordControl(context.Context, delivery.Job, string, int) error
	CompleteControl(context.Context, delivery.Job, string) error
}
type Telegram interface {
	SendText(context.Context, *bot.SendMessageParams) (*models.Message, error)
	Me(context.Context) (*models.User, error)
	AnswerCallback(context.Context, *bot.AnswerCallbackQueryParams) error
}
type Clients func(context.Context, string, string) (Telegram, error)
type Service struct {
	store      Store
	clients    Clients
	management int64
}

func New(store Store, clients Clients, management int64) (*Service, error) {
	if store == nil || clients == nil || management >= 0 {
		return nil, delivery.ErrInvalid
	}
	return &Service{store: store, clients: clients, management: management}, nil
}
