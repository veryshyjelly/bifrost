package bots

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

type ProvisionStore interface {
	PrepareProvisionControl(context.Context, delivery.Job, string, Settings) (control.Plan, error)
	PrepareCreatedBotControl(context.Context, delivery.Job, Settings) (control.Plan, error)
}
type Provisioner struct {
	store   ProvisionStore
	replies *control.Service
	master  interface {
		Me(context.Context) (*models.User, error)
	}
	settings Settings
}

func NewProvisioner(store ProvisionStore, replies *control.Service, master interface {
	Me(context.Context) (*models.User, error)
}, c Settings) (*Provisioner, error) {
	if store == nil || replies == nil || master == nil || c.OwnerID <= 0 || c.Management >= 0 || c.Session == "" {
		return nil, delivery.ErrInvalid
	}
	if c.CommandTTL == 0 {
		c.CommandTTL = 60
	}
	if c.ControlTTL == 0 {
		c.ControlTTL = 600
	}
	if c.CommandTTL < 5 || c.CommandTTL > 86400 || c.ControlTTL < 5 || c.ControlTTL > 86400 {
		return nil, delivery.ErrInvalid
	}
	return &Provisioner{store: store, replies: replies, master: master, settings: c}, nil
}
func (p *Provisioner) prepare(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if saved, found, err := p.replies.Prepared(ctx, j, j.Dedup); err != nil {
		return err
	} else if found {
		return p.replies.Send(ctx, j, e, saved)
	}
	if err := e.Permit(); err != nil {
		return err
	}
	identity, err := p.master.Me(ctx)
	if err != nil {
		return err
	}
	if identity == nil || identity.Username == "" || !identity.IsBot {
		return providers.InvalidResponse("telegram", false)
	}
	plan, err := p.store.PrepareProvisionControl(ctx, j, identity.Username, p.settings)
	if err != nil {
		return local(err)
	}
	return p.replies.Send(ctx, j, e, plan)
}
func (p *Provisioner) Handle(ctx context.Context, j delivery.Job, e *delivery.Effects, botID string, u *models.Update) error {
	if j.Kind != "tg" || e == nil || u == nil {
		return delivery.ErrInvalid
	}
	if m := u.EditedMessage; m != nil {
		if command, ok := control.ParseCommand(m); ok && command.Name == "create" {
			return nil
		}
		return delivery.ErrUnsupported
	}
	if q := u.CallbackQuery; q != nil {
		if !strings.HasPrefix(q.Data, "bind:") {
			return delivery.ErrUnsupported
		}
		if q.From.IsBot || q.From.ID != p.settings.OwnerID {
			return nil
		}
		return p.prepare(ctx, j, e)
	}
	command, ok := control.ParseCommand(u.Message)
	if !ok || command.Name != "create" {
		return delivery.ErrUnsupported
	}
	m := u.Message
	if m.From == nil || m.From.ID != p.settings.OwnerID || m.From.IsBot {
		return nil
	}
	if command.Recipient == "" && m.Chat.Type != models.ChatTypePrivate && botID != "master" {
		return nil
	}
	if saved, found, err := p.replies.Prepared(ctx, j, j.Dedup); err != nil {
		return err
	} else if found {
		return p.replies.Send(ctx, j, e, saved)
	}
	addressed, err := p.replies.Addressed(ctx, j, botID, command.Recipient)
	if err != nil || !addressed {
		return err
	}
	return p.prepare(ctx, j, e)
}
func (p *Provisioner) Manage(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "management" || e == nil {
		return delivery.ErrInvalid
	}
	var event control.OperatorEvent
	if json.Unmarshal(j.Payload, &event) != nil {
		return delivery.ErrInvalid
	}
	if event.Type != "operator_action" || event.Data.Operation != "create" {
		return delivery.ErrUnsupported
	}
	return p.prepare(ctx, j, e)
}
func (p *Provisioner) Created(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "control-bot-created" || e == nil {
		return delivery.ErrInvalid
	}
	plan, err := p.store.PrepareCreatedBotControl(ctx, j, p.settings)
	if err != nil {
		return local(err)
	}
	return p.replies.Send(ctx, j, e, plan)
}

// Operator composes contact creation with the existing preference operations.
// Unsupported capabilities stay explicit until the runtime provides them.
func (p *Provisioner) Operator(next delivery.Handler) delivery.Handler {
	return func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		err := p.Manage(ctx, j, e)
		if errors.Is(err, delivery.ErrUnsupported) && next != nil {
			return next(ctx, j, e)
		}
		return err
	}
}
