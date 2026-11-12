// Package operations provides owner diagnostics and passive operational notices.
package operations

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

type Settings struct {
	Session, SyncCron, SyncTimezone   string
	OwnerID, Management               int64
	CommandTTL, ControlTTL, NoticeTTL int
	Configured                        bool
}

func ValidSettings(c Settings) bool {
	return c.Session != "" && c.OwnerID > 0 && c.Management < 0 && c.CommandTTL >= 5 && c.CommandTTL <= 86400 && c.ControlTTL >= 5 && c.ControlTTL <= 86400 && c.NoticeTTL >= 5 && c.NoticeTTL <= 86400 && c.SyncTimezone != ""
}

type LastSync struct {
	Run           string `json:"run"`
	FinishedAt    int64  `json:"finishedAt"`
	Source        string `json:"source"`
	Targets       int    `json:"targets"`
	Groups        int    `json:"groups"`
	Contacts      int    `json:"contacts"`
	Changed       int    `json:"changed"`
	Warnings      int    `json:"warnings"`
	MissingBots   int    `json:"missingBots"`
	SkippedPhotos int    `json:"skippedPhotos"`
}
type JobCount struct {
	State string `json:"state"`
	Count int64  `json:"count"`
}
type Status struct {
	Configured    bool  `json:"configured"`
	Bots          int64 `json:"bots"`
	Conversations int64 `json:"conversations"`
	MetadataSync  struct {
		Cron     string    `json:"cron"`
		Timezone string    `json:"timezone"`
		Last     *LastSync `json:"last"`
	} `json:"metadataSync"`
	TelegramControls struct {
		CommandTTLSeconds int   `json:"commandTTLSeconds"`
		ButtonTTLSeconds  int   `json:"buttonTTLSeconds"`
		CleanupProblems   int64 `json:"cleanupProblems"`
	} `json:"telegramControls"`
	Jobs []JobCount `json:"jobs"`
}
type Store interface {
	PrepareOperationCommand(context.Context, delivery.Job, Settings, *bool) (control.Plan, error)
	PrepareOperationNotice(context.Context, delivery.Job, Settings) (control.Plan, error)
	QueueOperationSessionEvent(context.Context, delivery.Job, Settings) error
}
type Telegram interface {
	Chat(context.Context, *bot.GetChatParams) (*models.ChatFullInfo, error)
}
type Clients func(context.Context, string, string) (Telegram, error)
type Service struct {
	store   Store
	replies *control.Service
	clients Clients
	c       Settings
}

func New(store Store, replies *control.Service, clients Clients, c Settings) (*Service, error) {
	if store == nil || replies == nil || clients == nil || !ValidSettings(c) {
		return nil, delivery.ErrInvalid
	}
	return &Service{store: store, replies: replies, clients: clients, c: c}, nil
}
func local(err error) error {
	if err == nil || errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, delivery.ErrInvalid) || errors.Is(err, delivery.ErrUnsupported) || errors.Is(err, delivery.ErrDeferred) {
		return err
	}
	var outcome delivery.Outcome
	if errors.As(err, &outcome) {
		return err
	}
	return delivery.ErrLocalRetry
}
func (s *Service) Handle(ctx context.Context, j delivery.Job, e *delivery.Effects, botID string, u *models.Update) error {
	if j.Kind != "tg" || e == nil || u == nil {
		return delivery.ErrInvalid
	}
	m := u.Message
	if m == nil {
		m = u.EditedMessage
	}
	parsed, ok := control.ParseCommand(m)
	if !ok || (parsed.Name != "status" && parsed.Name != "inbox") || parsed.Name == "inbox" && botID != "master" {
		return delivery.ErrUnsupported
	}
	if m.From == nil || m.From.IsBot || m.From.ID != s.c.OwnerID || m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != s.c.OwnerID || botID != "master" && m.Chat.Type != models.ChatTypePrivate && parsed.Recipient == "" {
		return nil
	}
	if u.EditedMessage != nil {
		return nil
	}
	addressed, err := s.replies.Addressed(ctx, j, botID, parsed.Recipient)
	if err != nil {
		return err
	}
	if !addressed {
		return nil
	}
	if p, found, err := s.replies.Prepared(ctx, j, j.Dedup); err != nil {
		return err
	} else if found {
		return s.replies.Send(ctx, j, e, p)
	}
	var forum *bool
	if parsed.Name == "inbox" {
		if err := e.Permit(); err != nil {
			return err
		}
		client, err := s.clients(ctx, j.AccountID, botID)
		if err != nil {
			return local(err)
		}
		chat, err := client.Chat(ctx, &bot.GetChatParams{ChatID: m.Chat.ID})
		if err != nil {
			return err
		}
		if chat == nil || chat.ID != m.Chat.ID {
			return providers.InvalidResponse("telegram", false)
		}
		forum = &chat.IsForum
	}
	p, err := s.store.PrepareOperationCommand(ctx, j, s.c, forum)
	if err != nil {
		return local(err)
	}
	return s.replies.Send(ctx, j, e, p)
}
func (s *Service) Management(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "management" || e == nil {
		return delivery.ErrInvalid
	}
	var event struct{ Type string }
	if json.Unmarshal(j.Payload, &event) != nil {
		return delivery.ErrInvalid
	}
	if event.Type != "delivery_problem" && event.Type != "session_status" {
		return delivery.ErrUnsupported
	}
	p, err := s.store.PrepareOperationNotice(ctx, j, s.c)
	if err != nil {
		return local(err)
	}
	return s.replies.Send(ctx, j, e, p)
}
func (s *Service) SessionEvent(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "wa" || e == nil {
		return delivery.ErrInvalid
	}
	return local(s.store.QueueOperationSessionEvent(ctx, j, s.c))
}
