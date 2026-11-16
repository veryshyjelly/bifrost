// Package revocations keeps source tombstones and destination-scoped owner
// deletion choices separate from idempotent Telegram cleanup.
package revocations

import (
	"context"
	"errors"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

type Settings struct {
	Session               string
	OwnerID               int64
	ControlTTL, NoticeTTL int
}

// Copy identifies a delivered destination message that an owner may keep or delete.
type Copy struct {
	Target    control.Target
	MessageID int64
	Media     bool
}

// Task retains the owner-facing choice and frozen destination copies for one
// revocation. Cursor and Blocked preserve partial deletion progress across jobs.
type Task struct {
	ID, Scope, SourceSession, ChatID, MessageID, Status, Mode, NoticeBot string
	OwnerID, ExpiresAt, PromptMessageID                                  int64
	Own, Presented                                                       bool
	Primary                                                              Copy
	Copies                                                               []Copy
	Cursor, Blocked                                                      int
	NoticeTTL                                                            int
}

func (t Task) Markup() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: "Delete", CallbackData: "rev-delete:" + t.ID}, {Text: "Keep", CallbackData: "rev-keep:" + t.ID}}}}
}

type Job struct {
	ID, Stage string
	Cursor    int
}

// Store admits source tombstones and destination choices separately, then advances
// presentation, deletion and expiry under the current worker claim.
type Store interface {
	NormalizeChange(context.Context, delivery.Job, string) error
	AdmitRevocation(context.Context, delivery.Job, Settings) error
	PrepareRevocationCallback(context.Context, delivery.Job, Settings) (control.Plan, error)
	RevocationStep(context.Context, delivery.Job, Settings) (Task, bool, error)
	UseRevocationReply(context.Context, delivery.Job) error
	RevocationPrompt(context.Context, delivery.Job) (control.Plan, error)
	MarkRevocationPresented(context.Context, delivery.Job) error
	RefuseRevocationPrompt(context.Context, delivery.Job) error
	AdvanceRevocationDelete(context.Context, delivery.Job, bool) error
	BeginRevocationExpiry(context.Context, delivery.Job) (bool, error)
	FinishRevocation(context.Context, delivery.Job, bool) error
}
type Telegram interface {
	EditMarkup(context.Context, *bot.EditMessageReplyMarkupParams) error
	DeleteMessages(context.Context, *bot.DeleteMessagesParams) error
}
type Clients func(context.Context, string, string) (Telegram, error)
type Service struct {
	store   Store
	clients Clients
	replies *control.Service
	c       Settings
}

func New(store Store, clients Clients, replies *control.Service, c Settings) (*Service, error) {
	if c.ControlTTL == 0 {
		c.ControlTTL = 600
	}
	if c.NoticeTTL == 0 {
		c.NoticeTTL = 60
	}
	if store == nil || clients == nil || replies == nil || c.Session == "" || c.OwnerID <= 0 || c.ControlTTL < 5 || c.ControlTTL > 86400 || c.NoticeTTL < 5 || c.NoticeTTL > 86400 {
		return nil, delivery.ErrInvalid
	}
	return &Service{store: store, clients: clients, replies: replies, c: c}, nil
}
func local(err error) error {
	if err == nil || errors.Is(err, delivery.ErrInvalid) || errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, delivery.ErrUncertain) || errors.Is(err, delivery.ErrDeferred) || errors.Is(err, delivery.ErrUnsupported) {
		return err
	}
	var wait control.WaitUntil
	if errors.As(err, &wait) {
		return err
	}
	return delivery.ErrLocalRetry
}
