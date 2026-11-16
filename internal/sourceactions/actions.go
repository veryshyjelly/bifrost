// Package sourceactions admits explicit owner source mutations independently
// of command notices and preserves Telegram content on provider refusal.
package sourceactions

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

type Settings struct {
	Session               string
	OwnerID, Management   int64
	CommandTTL, NoticeTTL int
}

// Action freezes an admitted owner mutation and the original reply destination.
// It targets the WhatsApp source message independently of Telegram copy cleanup.
type Action struct {
	Key, Session, Operation, ChatID, MessageID, Emoji, State string
	Target                                                   control.Target
	NoticeTTL                                                int
}

// Store atomically admits source commands and exposes their saved action to the
// worker. Completion records success or a definitive provider refusal.
type Store interface {
	PrepareSourceCommand(context.Context, delivery.Job, Settings) (control.Plan, error)
	AdmitSourceManagement(context.Context, delivery.Job, Settings) error
	SourceAction(context.Context, delivery.Job) (Action, error)
	CompleteSourceAction(context.Context, delivery.Job, bool, int) error
}
type WhatsApp interface {
	React(context.Context, string, sdk.ReactMessageRequest) error
	DeleteMessage(context.Context, string, sdk.DeleteMessageRequest) error
}
type Service struct {
	store   Store
	wa      WhatsApp
	replies *control.Service
	c       Settings
}

func New(store Store, wa WhatsApp, replies *control.Service, c Settings) (*Service, error) {
	if c.CommandTTL == 0 {
		c.CommandTTL = 60
	}
	if c.NoticeTTL == 0 {
		c.NoticeTTL = 60
	}
	if store == nil || wa == nil || replies == nil || c.Session == "" || c.OwnerID <= 0 || c.Management >= 0 || c.CommandTTL < 5 || c.CommandTTL > 86400 || c.NoticeTTL < 5 || c.NoticeTTL > 86400 {
		return nil, delivery.ErrInvalid
	}
	return &Service{store: store, wa: wa, replies: replies, c: c}, nil
}
func command(name string) bool { return name == "react" || name == "delete" }
func (s *Service) Handle(ctx context.Context, j delivery.Job, e *delivery.Effects, id string, u *models.Update) error {
	if j.Kind != "tg" || e == nil || u == nil {
		return delivery.ErrInvalid
	}
	if m := u.EditedMessage; m != nil {
		if c, ok := control.ParseCommand(m); ok && command(c.Name) {
			return nil
		}
		return delivery.ErrUnsupported
	}
	m := u.Message
	parsed, ok := control.ParseCommand(m)
	if !ok || !command(parsed.Name) {
		return delivery.ErrUnsupported
	}
	if m.From == nil || m.From.IsBot || m.From.ID != s.c.OwnerID {
		return nil
	}
	if m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != s.c.OwnerID {
		return delivery.ErrInvalid
	}
	if parsed.Recipient == "" && m.Chat.Type != models.ChatTypePrivate && id != "master" {
		return nil
	}
	if p, known, err := s.replies.Prepared(ctx, j, j.Dedup); err != nil {
		return err
	} else if known {
		return s.replies.Send(ctx, j, e, p)
	}
	addressed, err := s.replies.Addressed(ctx, j, id, parsed.Recipient)
	if err != nil || !addressed {
		return err
	}
	p, err := s.store.PrepareSourceCommand(ctx, j, s.c)
	if err != nil {
		return local(err)
	}
	return s.replies.Send(ctx, j, e, p)
}
func (s *Service) Manage(ctx context.Context, j delivery.Job, _ *delivery.Effects) error {
	if j.Kind != "management" {
		return delivery.ErrInvalid
	}
	var source struct {
		Type string `json:"type"`
		Data struct {
			Operation string `json:"operation"`
		} `json:"data"`
	}
	if json.Unmarshal(j.Payload, &source) != nil {
		return delivery.ErrInvalid
	}
	if source.Type != "operator_action" || !command(source.Data.Operation) {
		return delivery.ErrUnsupported
	}
	return local(s.store.AdmitSourceManagement(ctx, j, s.c))
}

// Run resumes an admitted source mutation. Already terminal actions are skipped;
// retryable or uncertain provider errors remain unresolved for worker handling.
func (s *Service) Run(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "control-source-action" || e == nil {
		return delivery.ErrInvalid
	}
	p, err := s.store.SourceAction(ctx, j)
	if err != nil {
		return local(err)
	}
	if p.State == "done" || p.State == "refused" {
		return nil
	}
	if err := e.Permit(); err != nil {
		return err
	}
	switch p.Operation {
	case "react":
		err = s.wa.React(ctx, p.Session, sdk.ReactMessageRequest{ChatID: p.ChatID, MessageID: p.MessageID, Emoji: p.Emoji})
	case "delete":
		everyone := true
		err = s.wa.DeleteMessage(ctx, p.Session, sdk.DeleteMessageRequest{ChatID: p.ChatID, MessageID: p.MessageID, ForEveryone: &everyone})
	default:
		return delivery.ErrInvalid
	}
	if err != nil {
		var refusal *providers.Error
		if !errors.As(err, &refusal) || refusal.Retry || refusal.Uncertain {
			return err
		}
		switch refusal.Status {
		case 400, 403, 404, 413, 422, 501:
		default:
			return err
		}
		return local(s.store.CompleteSourceAction(ctx, j, false, refusal.Status))
	}
	return local(s.store.CompleteSourceAction(ctx, j, true, 0))
}
func local(err error) error {
	if err == nil || errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, delivery.ErrInvalid) || errors.Is(err, delivery.ErrUnsupported) || errors.Is(err, delivery.ErrDeferred) {
		return err
	}
	return delivery.ErrLocalRetry
}
