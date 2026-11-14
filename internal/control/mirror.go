package control

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

const UnmirrorHelp = "Send /unmirror to Bifrost inside the linked mirror group to move it back to a management topic."
const UnmirrorStale = "This group is no longer the active mirror. Use /unmirror in the current mirror group."
const UnmirrorBusy = "History is importing for this conversation. Wait for it to finish, then run /unmirror again."
const UnmirrorPermission = "Could not create a management topic. Check Bifrost’s Manage Topics permission in the management forum, then run /unmirror again."

type MirrorRequest struct {
	Operation, ChatID         string
	OwnerID, ManagementChatID int64
}
type MirrorEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		ChatID         string `json:"chatId"`
		TelegramChat   int64  `json:"tgChat"`
		PreviousReturn string `json:"previousReturn"`
		RequestID      string `json:"requestId,omitempty"`
	} `json:"data"`
	At int64 `json:"at"`
}

// UnmarshalJSON accepts the baseline string form of Telegram IDs without a
// floating-point conversion, as well as the native integer form.
func (e *MirrorEvent) UnmarshalJSON(b []byte) error {
	type wire MirrorEvent
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(b, &outer); err != nil {
		return err
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(outer["data"], &data); err != nil {
		return err
	}
	if raw := data["tgChat"]; len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		data["tgChat"] = []byte(strconv.FormatInt(n, 10))
	}
	d, err := json.Marshal(data)
	if err != nil {
		return err
	}
	outer["data"] = d
	b, err = json.Marshal(outer)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, (*wire)(e))
}

type Transition struct {
	Kind, ChatID, Name, PreviousReturn, Stage string
	FromChat, Management, OwnerID             int64
	ExpectedChat, ExpectedThread, ThreadID    int64
	TTL, ControlTTL                           int
	RequestID                                 int32 `json:",omitempty"`
}
type MirrorStore interface {
	PrepareTransition(context.Context, delivery.Job, int64, int64, int, int) (Transition, error)
	CommitMirrorLink(context.Context, delivery.Job) error
	CommitInboxReturn(context.Context, delivery.Job, int64) error
	RefuseInboxReturn(context.Context, delivery.Job) error
	MirrorNoticeCurrent(context.Context, delivery.Job) (bool, error)
	CancelMirrorNotice(context.Context, delivery.Job) error
}
type MirrorTelegram interface {
	Chat(context.Context, *bot.GetChatParams) (*models.ChatFullInfo, error)
	Me(context.Context) (*models.User, error)
	ChatMember(context.Context, *bot.GetChatMemberParams) (*models.ChatMember, error)
	CreateForumTopic(context.Context, *bot.CreateForumTopicParams) (*models.ForumTopic, error)
}
type Mirrors struct {
	store      MirrorStore
	replies    *Service
	master     MirrorTelegram
	owner      int64
	ttl        int
	controlTTL int
}
type MirrorOptions struct {
	OwnerID                int64
	CommandTTL, ControlTTL time.Duration
}

func NewMirrors(store MirrorStore, replies *Service, master MirrorTelegram, c MirrorOptions) (*Mirrors, error) {
	if c.CommandTTL == 0 {
		c.CommandTTL = time.Minute
	}
	if c.ControlTTL == 0 {
		c.ControlTTL = 10 * time.Minute
	}
	if store == nil || replies == nil || master == nil || c.OwnerID <= 0 || c.CommandTTL < 5*time.Second || c.CommandTTL > 24*time.Hour || c.CommandTTL%time.Second != 0 || c.ControlTTL < 5*time.Second || c.ControlTTL > 24*time.Hour || c.ControlTTL%time.Second != 0 {
		return nil, delivery.ErrInvalid
	}
	return &Mirrors{store: store, replies: replies, master: master, owner: c.OwnerID, ttl: int(c.CommandTTL / time.Second), controlTTL: int(c.ControlTTL / time.Second)}, nil
}

func (m *Mirrors) Handle(ctx context.Context, j delivery.Job, e *delivery.Effects, botID string, u *models.Update) error {
	if u == nil || j.Kind != "tg" {
		return delivery.ErrInvalid
	}
	if edited := u.EditedMessage; edited != nil {
		if p, ok := ParseCommand(edited); ok && (p.Name == "link" || p.Name == "unmirror") {
			return nil
		}
		known, err := m.replies.WasCommand(ctx, j, botID, edited)
		if err != nil || known {
			return err
		}
		return delivery.ErrUnsupported
	}
	s := u.Message
	p, ok := ParseCommand(s)
	if !ok || (p.Name != "link" && p.Name != "unmirror") {
		return delivery.ErrUnsupported
	}
	if s.From == nil || s.From.IsBot || s.From.ID != m.owner {
		return nil
	}
	if s.Chat.Type == models.ChatTypePrivate && s.Chat.ID != m.owner {
		return delivery.ErrInvalid
	}
	if old, known, err := m.replies.Prepared(ctx, j, j.Dedup); err != nil {
		return err
	} else if known {
		return m.replies.Send(ctx, j, e, old)
	}
	if p.Recipient == "" && s.Chat.Type != models.ChatTypePrivate && botID != "master" {
		return nil
	}
	addressed, err := m.replies.Addressed(ctx, j, botID, p.Recipient)
	if err != nil || !addressed {
		return err
	}
	c, err := m.replies.Context(ctx, j, botID, s)
	if err != nil {
		return err
	}
	plan := Plan{Key: j.Dedup, Target: c.Target, ReplyTTLSeconds: m.ttl, Command: &Command{MessageID: int64(s.ID), Date: int64(s.Date) * 1000, TTLSeconds: m.ttl}}
	group := s.Chat.Type == models.ChatTypeGroup || s.Chat.Type == models.ChatTypeSupergroup
	text := ""
	chat := ""
	if p.Name == "unmirror" {
		if botID != "master" || len(p.Args) > 0 || !group || s.Chat.ID == m.replies.management || !c.Known || !strings.HasSuffix(c.Route.ChatID, "@g.us") {
			text = UnmirrorHelp
		} else {
			chat = c.Route.ChatID
		}
	} else if botID == "master" && group && s.Chat.ID != m.replies.management {
		if len(p.Args) == 0 || !strings.HasSuffix(p.Args[0], "@g.us") {
			text = "Use /link <WhatsApp group ID ending in @g.us>. The ID is shown in its management topic."
		} else {
			chat = p.Args[0]
		}
	} else if c.Known {
		text = "Unknown command. Use /start in the master bot for help."
	} else {
		text = "Use this command inside a linked conversation or its management topic."
	}
	if text != "" {
		plan.Packets = []Packet{{Text: text}}
	} else {
		plan.Mirror = &MirrorRequest{Operation: p.Name, ChatID: chat, OwnerID: m.owner, ManagementChatID: m.replies.management}
	}
	return m.replies.Send(ctx, j, e, plan)
}

func (m *Mirrors) Manage(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if e == nil {
		return delivery.ErrInvalid
	}
	p, err := m.store.PrepareTransition(ctx, j, m.replies.management, m.owner, m.ttl, m.controlTTL)
	if err != nil {
		return local(err)
	}
	if p.Stage == "link" {
		if err := e.Permit(); err != nil {
			return err
		}
		chat, err := m.master.Chat(ctx, &bot.GetChatParams{ChatID: p.FromChat})
		if err != nil {
			return err
		}
		if chat == nil || chat.ID != p.FromChat {
			return providers.InvalidResponse("telegram", false)
		}
		if (chat.Type != models.ChatTypeGroup && chat.Type != models.ChatTypeSupergroup) || chat.Username != "" || len(chat.ActiveUsernames) > 0 {
			return delivery.ErrInvalid
		}
		me, err := m.master.Me(ctx)
		if err != nil {
			return err
		}
		if me == nil || me.ID <= 0 || !me.IsBot {
			return providers.InvalidResponse("telegram", false)
		}
		member, err := m.master.ChatMember(ctx, &bot.GetChatMemberParams{ChatID: p.FromChat, UserID: me.ID})
		if err != nil {
			return err
		}
		if member == nil || member.Type != models.ChatMemberTypeAdministrator {
			return delivery.ErrInvalid
		}
		if member.Administrator == nil || member.Administrator.User.ID != me.ID {
			return providers.InvalidResponse("telegram", false)
		}
		if err := m.store.CommitMirrorLink(ctx, j); err != nil {
			return local(err)
		}
	} else if p.Stage == "create" {
		request, _ := json.Marshal(struct {
			ChatID int64  `json:"chat_id"`
			Name   string `json:"name"`
		}{p.Management, conversation.TopicName(p.Name, p.ChatID)})
		result, err := e.Do(j.Dedup+":topic", request, func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var frozen struct {
				ChatID int64  `json:"chat_id"`
				Name   string `json:"name"`
			}
			if json.Unmarshal(raw, &frozen) != nil {
				return nil, delivery.ErrInvalid
			}
			topic, err := m.master.CreateForumTopic(ctx, &bot.CreateForumTopicParams{ChatID: frozen.ChatID, Name: frozen.Name})
			if err != nil {
				return nil, err
			}
			if topic == nil || topic.MessageThreadID <= 1 {
				return nil, providers.InvalidResponse("telegram", true)
			}
			return json.Marshal(topic)
		})
		if err != nil {
			var refusal *providers.Error
			if !errors.Is(err, delivery.ErrDiscarded) {
				if !errors.As(err, &refusal) || refusal.Retry || refusal.Uncertain || (refusal.Status != 400 && refusal.Status != 403) {
					return err
				}
				if err := e.Discard(j.Dedup + ":topic"); err != nil {
					return err
				}
			}
			if err := m.store.RefuseInboxReturn(ctx, j); err != nil {
				return local(err)
			}
		} else {
			var topic models.ForumTopic
			if json.Unmarshal(result, &topic) != nil || topic.MessageThreadID <= 1 {
				return delivery.ErrUncertain
			}
			if err := m.store.CommitInboxReturn(ctx, j, int64(topic.MessageThreadID)); err != nil {
				return local(err)
			}
		}
	}
	current, err := m.store.MirrorNoticeCurrent(ctx, j)
	if err != nil {
		return local(err)
	}
	if !current {
		return local(m.store.CancelMirrorNotice(ctx, j))
	}
	plan, known, err := m.replies.Prepared(ctx, j, j.Dedup)
	if err != nil {
		return err
	}
	if !known {
		return delivery.ErrInvalid
	}
	return m.replies.Send(ctx, j, e, plan)
}
