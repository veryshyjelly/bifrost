package control

import (
	"context"
	"time"

	"github.com/go-telegram/bot"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

type ClearRequest struct {
	ID, Mode                  string
	OwnerID, ManagementChatID int64
}
type ClearRun struct {
	ID, Scope, Mode, Status                   string
	Target                                    Target
	Cutoff, Cursor, Removed, Skipped, Blocked int64
	Single                                    bool
}
type CleanupItem struct{ Seq, MessageID int64 }
type CleanupStep struct {
	Key, Kind, ClearID, Outcome string
	Target                      Target
	Scope                       string
	Items                       []CleanupItem
	Skipped, Gone               []int64
	Cursor                      int64
	Run                         ClearRun
}
type CleanupStore interface {
	PrepareExpiry(context.Context, delivery.Job) (CleanupStep, error)
	FinishExpiry(context.Context, delivery.Job, string) error
	PrepareClearStep(context.Context, delivery.Job) (CleanupStep, error)
	FinishClearStep(context.Context, delivery.Job, string) error
	FinishClearRun(context.Context, delivery.Job) error
}
type DeleteTelegram interface {
	DeleteMessages(context.Context, *bot.DeleteMessagesParams) error
}
type DeleteClients func(context.Context, string, string) (DeleteTelegram, error)
type CleanupOptions struct {
	OwnerID    int64
	CommandTTL time.Duration
}
type Cleaner struct {
	store   CleanupStore
	replies *Service
	clients DeleteClients
	c       CleanupOptions
}

func NewCleaner(store CleanupStore, replies *Service, clients DeleteClients, c CleanupOptions) (*Cleaner, error) {
	if c.CommandTTL == 0 {
		c.CommandTTL = 60 * time.Second
	}
	if store == nil || replies == nil || clients == nil || c.OwnerID <= 0 || c.CommandTTL < 5*time.Second || c.CommandTTL > 24*time.Hour || c.CommandTTL%time.Second != 0 {
		return nil, delivery.ErrInvalid
	}
	return &Cleaner{store: store, replies: replies, clients: clients, c: c}, nil
}

// WaitUntil handles a changed deadline without performing an early deletion.
type WaitUntil struct{ Deadline time.Time }

func (WaitUntil) Error() string { return "cleanup deadline has not arrived" }
func (w WaitUntil) DeliveryOutcome() (bool, bool, time.Duration) {
	return true, false, max(time.Second, time.Until(w.Deadline))
}
