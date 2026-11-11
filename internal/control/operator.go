package control

import (
	"context"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

// OperatorRequest is admitted with the source command. Its management job is
// independent of the source update and any relay/send receipt.
type OperatorRequest struct {
	Operation                 string
	Context                   Context
	OwnerID, ManagementChatID int64
}

type OperatorEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Operation string `json:"operation"`
		ChatID    string `json:"chatId"`
		Sender    string `json:"sender"`
	} `json:"data"`
	At int64 `json:"at"`
}

type OperatorStore interface {
	OperatorContext(context.Context, delivery.Job, string, *models.Message, int64) (Context, error)
	PrepareOperatorControl(context.Context, delivery.Job, int) (Plan, error)
}

type OperatorOptions struct {
	OwnerID    int64
	CommandTTL time.Duration
}

type Operator struct {
	store   OperatorStore
	replies *Service
	c       OperatorOptions
}

func NewOperator(store OperatorStore, replies *Service, c OperatorOptions) (*Operator, error) {
	if c.CommandTTL == 0 {
		c.CommandTTL = time.Minute
	}
	if store == nil || replies == nil || c.OwnerID <= 0 || c.CommandTTL < 5*time.Second || c.CommandTTL > 24*time.Hour || c.CommandTTL%time.Second != 0 {
		return nil, delivery.ErrInvalid
	}
	return &Operator{store: store, replies: replies, c: c}, nil
}

func IsOperatorCommand(name string) bool {
	return name == "keep" || name == "mute" || name == "ignore" || name == "read"
}

func (o *Operator) Handle(ctx context.Context, job delivery.Job, e *delivery.Effects, botID string, u *models.Update) error {
	if job.Kind != "tg" || u == nil || botID == "" {
		return delivery.ErrInvalid
	}
	if edited := u.EditedMessage; edited != nil {
		if parsed, ok := ParseCommand(edited); ok && IsOperatorCommand(parsed.Name) {
			return nil
		}
		known, err := o.replies.WasCommand(ctx, job, botID, edited)
		if err != nil || known {
			return err
		}
		return delivery.ErrUnsupported
	}
	m := u.Message
	parsed, ok := ParseCommand(m)
	if !ok || !IsOperatorCommand(parsed.Name) {
		return delivery.ErrUnsupported
	}
	if m.From == nil || m.From.IsBot || m.From.ID != o.c.OwnerID {
		return nil
	}
	if m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != o.c.OwnerID {
		return delivery.ErrInvalid
	}
	if saved, ok, err := o.replies.Prepared(ctx, job, job.Dedup); err != nil {
		return err
	} else if ok {
		return o.replies.Send(ctx, job, e, saved)
	}
	if parsed.Recipient == "" && m.Chat.Type != models.ChatTypePrivate && botID != "master" {
		return nil
	}
	addressed, err := o.replies.Addressed(ctx, job, botID, parsed.Recipient)
	if err != nil || !addressed {
		return err
	}
	c, err := o.store.OperatorContext(ctx, job, botID, m, o.replies.management)
	if err != nil {
		return local(err)
	}
	p := Plan{Key: job.Dedup, Target: c.Target, ReplyTTLSeconds: int(o.c.CommandTTL / time.Second), Command: &Command{MessageID: int64(m.ID), Date: int64(m.Date) * 1000, TTLSeconds: int(o.c.CommandTTL / time.Second)}}
	if !c.Known {
		p.Packets = []Packet{{Text: "Use this command inside a linked conversation or its management topic."}}
	} else if c.Route.ChatID == "status@broadcast" {
		// Status author mutes have their own workflow; these are not ordinary
		// conversation preferences or permission to mark the feed read.
		return delivery.ErrUnsupported
	} else {
		p.Operator = &OperatorRequest{Operation: parsed.Name, Context: c, OwnerID: o.c.OwnerID, ManagementChatID: o.replies.management}
	}
	return o.replies.Send(ctx, job, e, p)
}

// Manage commits a preference change and its exact confirmation plan together.
// Retrying a notice never re-applies a mode over a later owner decision.
func (o *Operator) Manage(ctx context.Context, job delivery.Job, e *delivery.Effects) error {
	p, err := o.store.PrepareOperatorControl(ctx, job, int(o.c.CommandTTL/time.Second))
	if err != nil {
		return local(err)
	}
	return o.replies.Send(ctx, job, e, p)
}
