package control

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

const MasterHelp = "WhatsApp bridge ready. Use /statuses for WhatsApp Status posts and /muted_statuses to manage Status mutes. In the Status topic, reply to a post with /mute or /unmute_status. Use /sync all here for every group and contact bot. In a conversation: /create, /read, /history 50, /keep, /mute, /ignore, /group. Use /unmirror in a mirror group to move it back to management. Reply with /delete or /react 👍. /clear log removes notices; /clear all clears the current management topic, including General. Commands and notices disappear automatically. Use /status for delivery and sync state."
const MasterGroupHelp = "Use /create while replying to a participant, /sync for metadata, /group for a mirror group, or /clear log for notices. Use /clear all in management to clear only the current topic, including General. Open Bifrost privately for full help."
const MemberReady = "Ready. Messages here will be sent to your linked WhatsApp contact. Use /history 50 to import recent messages here."
const MemberUnbound = "Connect this bot to a WhatsApp contact in the master bot first: select the contact in its binding message, then return here."
const MemberGroupHelp = "Open this member bot privately to chat with its WhatsApp contact."

var mirrorStart = regexp.MustCompile(`^mirror_[a-f0-9-]+$`)

// DelegatedStart reserves existing deep links for their dedicated workflows.
func DelegatedStart(bot string, private bool, args []string) bool {
	if bot != "master" || len(args) == 0 {
		return false
	}
	if private {
		return mirrorStart.MatchString(args[0])
	}
	return strings.HasPrefix(args[0], "sync_")
}

type StartRequest struct {
	OwnerID, ManagementChatID int64
	ControlTTLSeconds         int
}
type StartOptions struct {
	OwnerID                int64
	CommandTTL, ControlTTL time.Duration
}
type Starter struct {
	replies *Service
	c       StartOptions
}

func NewStarter(replies *Service, c StartOptions) (*Starter, error) {
	if c.CommandTTL == 0 {
		c.CommandTTL = time.Minute
	}
	if c.ControlTTL == 0 {
		c.ControlTTL = 10 * time.Minute
	}
	if replies == nil || c.OwnerID <= 0 || c.CommandTTL < 5*time.Second || c.CommandTTL > 24*time.Hour || c.ControlTTL < 5*time.Second || c.ControlTTL > 24*time.Hour || c.CommandTTL%time.Second != 0 || c.ControlTTL%time.Second != 0 {
		return nil, delivery.ErrInvalid
	}
	return &Starter{replies: replies, c: c}, nil
}

func (s *Starter) Handle(ctx context.Context, job delivery.Job, e *delivery.Effects, bot string, u *models.Update) error {
	if u == nil || job.Kind != "tg" || bot == "" {
		return delivery.ErrInvalid
	}
	if edited := u.EditedMessage; edited != nil {
		if p, ok := ParseCommand(edited); ok && p.Name == "start" {
			return nil
		}
		known, err := s.replies.WasCommand(ctx, job, bot, edited)
		if err != nil || known {
			return err
		}
		return delivery.ErrUnsupported
	}
	m := u.Message
	p, ok := ParseCommand(m)
	if !ok || p.Name != "start" {
		return delivery.ErrUnsupported
	}
	if m.From == nil || m.From.IsBot || m.From.ID != s.c.OwnerID {
		return nil
	}
	if m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != s.c.OwnerID {
		return delivery.ErrInvalid
	}
	if saved, known, err := s.replies.Prepared(ctx, job, job.Dedup); err != nil {
		return err
	} else if known {
		return s.replies.Send(ctx, job, e, saved)
	}
	if p.Recipient == "" && m.Chat.Type != models.ChatTypePrivate && bot != "master" {
		return nil
	}
	addressed, err := s.replies.Addressed(ctx, job, bot, p.Recipient)
	if err != nil || !addressed {
		return err
	}
	if DelegatedStart(bot, m.Chat.Type == models.ChatTypePrivate, p.Args) {
		return delivery.ErrUnsupported
	}
	c, err := s.replies.Context(ctx, job, bot, m)
	if err != nil {
		return err
	}
	plan := Plan{Key: job.Dedup, Target: c.Target, ReplyTTLSeconds: int(s.c.CommandTTL / time.Second), Command: &Command{MessageID: int64(m.ID), Date: int64(m.Date) * 1000, TTLSeconds: int(s.c.CommandTTL / time.Second)}, Start: &StartRequest{OwnerID: s.c.OwnerID, ManagementChatID: s.replies.management, ControlTTLSeconds: int(s.c.ControlTTL / time.Second)}}
	return s.replies.Send(ctx, job, e, plan)
}
