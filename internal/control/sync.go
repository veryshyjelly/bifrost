package control

import (
	"context"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

const SyncPermissionExpired = "This permission link is no longer active for this group. Use /sync in the correct group for a fresh link."
const SyncPermissionQueued = "Checking group permissions and queuing its metadata sync."

type SyncPermission struct {
	ID, AccountID, Session, WhatsAppChat, ReturnKey string
	OwnerID, ChatID, ExpiresAt                      int64
}
type SyncSettings struct {
	Session                string
	OwnerID, Management    int64
	CommandTTL, ControlTTL int
}
type SyncOptions struct {
	Session                string
	OwnerID                int64
	CommandTTL, ControlTTL time.Duration
}
type SyncStore interface {
	PrepareSyncControl(context.Context, delivery.Job, SyncSettings) (Plan, error)
}
type Sync struct {
	store    SyncStore
	replies  *Service
	settings SyncSettings
}

func NewSync(store SyncStore, replies *Service, c SyncOptions) (*Sync, error) {
	if c.CommandTTL == 0 {
		c.CommandTTL = time.Minute
	}
	if c.ControlTTL == 0 {
		c.ControlTTL = 10 * time.Minute
	}
	if store == nil || replies == nil || c.Session == "" || c.OwnerID <= 0 || c.CommandTTL < 5*time.Second || c.CommandTTL > 24*time.Hour || c.ControlTTL < 5*time.Second || c.ControlTTL > 24*time.Hour || c.CommandTTL%time.Second != 0 || c.ControlTTL%time.Second != 0 {
		return nil, delivery.ErrInvalid
	}
	return &Sync{store: store, replies: replies, settings: SyncSettings{Session: c.Session, OwnerID: c.OwnerID, Management: replies.management, CommandTTL: int(c.CommandTTL / time.Second), ControlTTL: int(c.ControlTTL / time.Second)}}, nil
}
func (s *Sync) Handle(ctx context.Context, j delivery.Job, e *delivery.Effects, botID string, u *models.Update) error {
	if u == nil || j.Kind != "tg" || botID == "" {
		return delivery.ErrInvalid
	}
	if u.EditedMessage != nil {
		if p, ok := ParseCommand(u.EditedMessage); ok && (p.Name == "sync" || p.Name == "start" && len(p.Args) > 0 && strings.HasPrefix(p.Args[0], "sync_")) {
			return nil
		}
		return delivery.ErrUnsupported
	}
	m := u.Message
	p, ok := ParseCommand(m)
	grant := ok && p.Name == "start" && botID == "master" && m.Chat.Type != models.ChatTypePrivate && len(p.Args) > 0 && strings.HasPrefix(p.Args[0], "sync_")
	if !ok || p.Name != "sync" && !grant {
		return delivery.ErrUnsupported
	}
	if m.From == nil || m.From.IsBot || m.From.ID != s.settings.OwnerID {
		return nil
	}
	if m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != s.settings.OwnerID {
		return delivery.ErrInvalid
	}
	if saved, known, err := s.replies.Prepared(ctx, j, j.Dedup); err != nil {
		return err
	} else if known {
		return s.replies.Send(ctx, j, e, saved)
	}
	if p.Recipient == "" && botID != "master" && m.Chat.Type != models.ChatTypePrivate {
		return nil
	}
	addressed, err := s.replies.Addressed(ctx, j, botID, p.Recipient)
	if err != nil || !addressed {
		return err
	}
	plan, err := s.store.PrepareSyncControl(ctx, j, s.settings)
	if err != nil {
		return local(err)
	}
	return s.replies.Send(ctx, j, e, plan)
}
