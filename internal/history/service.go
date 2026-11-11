package history

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

type Service struct {
	store    Store
	wa       WhatsApp
	delivery Delivery
	replies  *control.Service
	c        Settings
}

func New(store Store, wa WhatsApp, send Delivery, replies *control.Service, c Settings) (*Service, error) {
	if c.DefaultCount == 0 {
		c.DefaultCount = 20
	}
	if c.CommandTTL == 0 {
		c.CommandTTL = 60
	}
	if c.ControlTTL == 0 {
		c.ControlTTL = 600
	}
	if c.Timezone == "" {
		c.Timezone = "Asia/Kolkata"
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return nil, delivery.ErrInvalid
	}
	if store == nil || wa == nil || send == nil || replies == nil || c.Session == "" || c.OwnerID <= 0 || c.Management >= 0 || c.DefaultCount < 1 || c.DefaultCount > 2000 || c.CommandTTL < 5 || c.CommandTTL > 86400 || c.ControlTTL < 5 || c.ControlTTL > 86400 {
		return nil, delivery.ErrInvalid
	}
	return &Service{store: store, wa: wa, delivery: send, replies: replies, c: c}, nil
}
func (s *Service) Handle(ctx context.Context, j delivery.Job, e *delivery.Effects, botID string, u *models.Update) error {
	if j.Kind != "tg" || e == nil || u == nil {
		return delivery.ErrInvalid
	}
	if m := u.EditedMessage; m != nil {
		if c, ok := control.ParseCommand(m); ok && c.Name == "history" {
			return nil
		}
		return delivery.ErrUnsupported
	}
	if q := u.CallbackQuery; q != nil {
		if !Callback(q.Data) {
			return delivery.ErrUnsupported
		}
		if q.From.IsBot || q.From.ID != s.c.OwnerID {
			return nil
		}
	} else {
		command, ok := control.ParseCommand(u.Message)
		if !ok || command.Name != "history" {
			return delivery.ErrUnsupported
		}
		m := u.Message
		if m.From == nil || m.From.IsBot || m.From.ID != s.c.OwnerID {
			return nil
		}
		if command.Recipient == "" && m.Chat.Type != models.ChatTypePrivate && botID != "master" {
			return nil
		}
		if p, known, err := s.replies.Prepared(ctx, j, j.Dedup); err != nil {
			return err
		} else if known {
			return s.replies.Send(ctx, j, e, p)
		}
		addressed, err := s.replies.Addressed(ctx, j, botID, command.Recipient)
		if err != nil || !addressed {
			return err
		}
	}
	p, err := s.store.PrepareHistoryControl(ctx, j, s.c)
	if err != nil {
		return local(err)
	}
	return s.replies.Send(ctx, j, e, p)
}
func (s *Service) Manage(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "management" || e == nil {
		return delivery.ErrInvalid
	}
	var event control.OperatorEvent
	if json.Unmarshal(j.Payload, &event) != nil {
		return delivery.ErrInvalid
	}
	if event.Type != "operator_action" || event.Data.Operation != "history" {
		return delivery.ErrUnsupported
	}
	p, err := s.store.PrepareHistoryPrompt(ctx, j, s.c)
	if err != nil {
		return local(err)
	}
	return s.replies.Send(ctx, j, e, p)
}
func (s *Service) Fetch(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "history" || e == nil {
		return delivery.ErrInvalid
	}
	run, fetch, err := s.store.HistoryFetch(ctx, j, s.c)
	if err != nil {
		return local(err)
	}
	if !fetch {
		return nil
	}
	if err := e.Permit(); err != nil {
		return err
	}
	batch, err := s.wa.History(ctx, s.c.Session, run.ChatID, run.Count)
	if err != nil {
		return err
	}
	return local(s.store.SaveHistoryBatch(ctx, j, batch, s.c))
}
func (s *Service) Item(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "history-message" || e == nil {
		return delivery.ErrInvalid
	}
	run, item, send, err := s.store.PrepareHistoryItem(ctx, j, s.c)
	if err != nil {
		return local(err)
	}
	if !send {
		return nil
	}
	if err := s.delivery.DeliverHistory(ctx, j, e, run, item); err != nil {
		return err
	}
	return local(s.store.CompleteHistoryItem(ctx, j))
}
func (s *Service) Finish(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "history-finish" || e == nil {
		return delivery.ErrInvalid
	}
	p, err := s.store.PrepareHistoryFinish(ctx, j, s.c)
	if err != nil {
		return local(err)
	}
	return s.replies.Send(ctx, j, e, p)
}
func (s *Service) Handlers() map[string]delivery.Handler {
	return map[string]delivery.Handler{"history": s.Fetch, "history-message": s.Item, "history-finish": s.Finish}
}
