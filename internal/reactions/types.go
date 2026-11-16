package reactions

import (
	"context"
	"errors"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

type Settings struct {
	Session   string
	OwnerID   int64
	NoticeTTL int
}

// Input captures actor state, destination caches and bot bindings used to build
// one incoming reaction plan without resolving them again during delivery.
type Input struct {
	Source             Event
	State              State
	Destinations       []Destination
	Caches             map[string]Cache
	Bindings           map[string]string
	SelfID, SenderName string
}
type Step struct {
	Kind, DestinationKey, BotID, Emoji, Actor, State string
	Target                                           control.Target
	MessageID, Order                                 int64
	Prior                                            *Applied
	ClearRefusal                                     bool
}

// Plan freezes destination groups and ordered reaction steps. Saved per-step
// progress lets a retry resume the same actor-to-bot choices.
type Plan struct {
	Key       string
	Input     Input
	Groups    []Group
	Steps     []Step
	NoticeTTL int
	Complete  bool `json:"-"`
}
type OutgoingInput struct {
	BotID, ChatID, MessageID, SelfID string
	Target                           control.Target
	TelegramMessageID                int64
	Update                           models.MessageReactionUpdated
	Mapped                           bool
	FrozenChoice                     *FrozenChoice `json:",omitempty"`
}

// FrozenChoice preserves an earlier resolution independently of send state.
type FrozenChoice struct {
	Emoji     string
	Supported bool
}
type OutgoingPlan struct {
	Key, Session, Emoji, CustomID, State string
	Supported                            bool
	NoticeTTL                            int
	Input                                OutgoingInput
}

// Store separates frozen incoming/outgoing reaction choices from delivery
// progress and keeps all mutation methods bound to the current worker claim.
type Store interface {
	NormalizeChange(context.Context, delivery.Job, string) error
	ReactionPlan(context.Context, string, string) (Plan, bool, error)
	ReactionInput(context.Context, delivery.Job, Settings) (Input, error)
	PrepareReactions(context.Context, delivery.Job, Plan, Settings) (Plan, error)
	AdvanceReaction(context.Context, delivery.Job, int) error
	RefuseReaction(context.Context, delivery.Job, int, int, string) error
	CompleteReactions(context.Context, delivery.Job) error
	OutgoingReactionPlan(context.Context, string, string) (OutgoingPlan, bool, error)
	OutgoingReactionInput(context.Context, delivery.Job, Settings) (OutgoingInput, error)
	PrepareOutgoingReaction(context.Context, delivery.Job, OutgoingPlan, Settings) (OutgoingPlan, error)
	CompleteOutgoingReaction(context.Context, delivery.Job) error
	CustomReactionEmoji(context.Context, string, string) (string, bool, error)
}
type Routing interface {
	SelectTarget(context.Context, string, conversation.Route, string, conversation.Flags) (conversation.Target, error)
}
type Telegram interface {
	SetReaction(context.Context, *bot.SetMessageReactionParams) error
	CustomEmoji(context.Context, string) ([]*models.Sticker, error)
}
type Clients func(context.Context, string, string) (Telegram, error)
type WhatsApp interface {
	React(context.Context, string, sdk.ReactMessageRequest) error
}
type Service struct {
	store   Store
	routing Routing
	clients Clients
	wa      WhatsApp
	c       Settings
}

func New(store Store, routing Routing, clients Clients, wa WhatsApp, c Settings) (*Service, error) {
	if c.NoticeTTL == 0 {
		c.NoticeTTL = 60
	}
	if store == nil || routing == nil || clients == nil || wa == nil || c.Session == "" || c.OwnerID <= 0 || c.NoticeTTL < 5 || c.NoticeTTL > 86400 {
		return nil, delivery.ErrInvalid
	}
	return &Service{store: store, routing: routing, clients: clients, wa: wa, c: c}, nil
}
func local(err error) error {
	if err == nil || errors.Is(err, delivery.ErrInvalid) || errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, delivery.ErrUncertain) || errors.Is(err, delivery.ErrDeferred) || errors.Is(err, delivery.ErrUnsupported) {
		return err
	}
	return delivery.ErrLocalRetry
}
