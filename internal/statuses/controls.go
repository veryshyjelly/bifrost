package statuses

import (
	"context"
	"errors"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

type Control struct {
	ID, Kind, Author, View                                          string
	OwnerID, ChatID, ThreadID, Revision, ExpiresAt, PresentationJob int64
	Page                                                            int
}
type Edit struct {
	Target    control.Target
	MessageID int64
	Body      presentation.Value
	Markup    models.InlineKeyboardMarkup
	Complete  bool
	Presented bool
}
type ControlStore interface {
	StatusTopic(context.Context, string, int64) (Feed, bool, error)
	StatusReaction(context.Context, string, string, int64, int64) (bool, error)
	PrepareStatusCommand(context.Context, delivery.Job, Settings) (control.Plan, error)
	PrepareStatusCallback(context.Context, delivery.Job, Settings) (control.Plan, error)
	CompleteStatusCallback(context.Context, delivery.Job) error
	StatusControlEdit(context.Context, delivery.Job) (Edit, error)
	CompleteStatusControlEdit(context.Context, delivery.Job, bool) error
}
type ControlTelegram interface {
	AnswerCallback(context.Context, *bot.AnswerCallbackQueryParams) error
	SetText(context.Context, *bot.EditMessageTextParams) error
}
type ControlClients func(context.Context, string, string) (ControlTelegram, error)
type Controls struct {
	archive *Service
	store   ControlStore
	replies *control.Service
	clients ControlClients
	c       Settings
}

func NewControls(store ControlStore, replies *control.Service, clients ControlClients, archive *Service, c Settings) (*Controls, error) {
	if store == nil || replies == nil || clients == nil || archive == nil || archive.c != c || !ValidSettings(c) {
		return nil, delivery.ErrInvalid
	}
	return &Controls{store: store, replies: replies, clients: clients, archive: archive, c: c}, nil
}
func (s *Controls) Handle(ctx context.Context, j delivery.Job, e *delivery.Effects, botID string, u *models.Update) error {
	if j.Kind != "tg" || e == nil || u == nil {
		return delivery.ErrInvalid
	}
	if u.CallbackQuery != nil && strings.HasPrefix(u.CallbackQuery.Data, "statusctl:") {
		return s.callback(ctx, j, e, u.CallbackQuery)
	}
	if r := u.MessageReaction; r != nil {
		passive, err := s.store.StatusReaction(ctx, j.AccountID, botID, r.Chat.ID, int64(r.MessageID))
		if err != nil {
			return local(err)
		}
		if passive {
			return nil
		}
		return delivery.ErrUnsupported
	}
	m := u.Message
	if m == nil {
		m = u.EditedMessage
	}
	if m == nil {
		return delivery.ErrUnsupported
	}
	feed, found, err := s.store.StatusTopic(ctx, j.AccountID, s.c.Management)
	if err != nil {
		return local(err)
	}
	inTopic := found && m.Chat.ID == feed.Management && int64(m.MessageThreadID) == feed.ThreadID
	parsed, command := control.ParseCommand(m)
	statusCommand := command && (parsed.Name == "statuses" || parsed.Name == "muted_statuses" || parsed.Name == "mute_status" || parsed.Name == "unmute_status" || inTopic && (parsed.Name == "mute" || parsed.Name == "unmute" || parsed.Name == "keep"))
	if !statusCommand && !inTopic {
		return delivery.ErrUnsupported
	}
	if botID != "master" || m.From == nil || m.From.IsBot || m.From.ID != s.c.OwnerID || m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != s.c.OwnerID {
		return nil
	}
	if u.EditedMessage != nil {
		return nil
	}
	if command {
		// Other commands retain their dedicated handlers (including /clear).
		if !statusCommand {
			return delivery.ErrUnsupported
		}
		addressed, err := s.replies.Addressed(ctx, j, botID, parsed.Recipient)
		if err != nil {
			return err
		}
		if !addressed {
			return nil
		}
	}
	if statusCommand && parsed.Name == "statuses" {
		if _, err := s.archive.topic(ctx, j, e); err != nil {
			return err
		}
	}
	p, err := s.store.PrepareStatusCommand(ctx, j, s.c)
	if err != nil {
		return local(err)
	}
	return s.replies.Send(ctx, j, e, p)
}
func (s *Controls) callback(ctx context.Context, j delivery.Job, e *delivery.Effects, q *models.CallbackQuery) error {
	if q.From.IsBot || q.From.ID != s.c.OwnerID {
		return nil
	}
	p, err := s.store.PrepareStatusCallback(ctx, j, s.c)
	if err != nil {
		return local(err)
	}
	if p.Complete {
		return nil
	}
	if p.Answer == nil {
		return delivery.ErrInvalid
	}
	if err := e.Permit(); err != nil {
		return err
	}
	client, err := s.clients(ctx, j.AccountID, p.Target.BotID)
	if err != nil {
		return local(err)
	}
	err = client.AnswerCallback(ctx, p.Answer)
	if err != nil {
		var refused *providers.Error
		if !errors.As(err, &refused) || refused.Retry || refused.Uncertain || (refused.Status != 400 && refused.Status != 403) {
			return err
		}
	}
	return local(s.store.CompleteStatusCallback(ctx, j))
}
func (s *Controls) Edit(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "control-status-edit" || e == nil {
		return delivery.ErrInvalid
	}
	p, err := s.store.StatusControlEdit(ctx, j)
	if err != nil {
		return local(err)
	}
	if p.Complete {
		return nil
	}
	if err := e.Permit(); err != nil {
		return err
	}
	client, err := s.clients(ctx, j.AccountID, p.Target.BotID)
	if err != nil {
		return local(err)
	}
	err = client.SetText(ctx, &bot.EditMessageTextParams{ChatID: p.Target.ChatID, MessageID: int(p.MessageID), Text: p.Body.Text, Entities: p.Body.Entities, ReplyMarkup: &p.Markup, LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: bot.True()}})
	if err != nil {
		var refused *providers.Error
		if !errors.As(err, &refused) || refused.Retry || refused.Uncertain || (refused.Status != 400 && refused.Status != 403) {
			return err
		}
	}
	return local(s.store.CompleteStatusControlEdit(ctx, j, err == nil))
}
func (s *Controls) Handlers() map[string]delivery.Handler {
	return map[string]delivery.Handler{"control-status-edit": s.Edit}
}
