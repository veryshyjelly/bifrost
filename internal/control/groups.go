package control

import (
	"context"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

const GroupButtonExpired = "This group button expired or its conversation changed. Use /group in the WhatsApp group’s inbox topic for a fresh button."
const GroupSelectionExpired = "This group selection is no longer active. Use /group in the WhatsApp group’s inbox topic to start again."
const GroupButtonText = "Create a private Telegram group for this conversation, or select one you own. Telegram will ask you to confirm adding Bifrost as an administrator."

type MirrorChoice struct {
	ID, AccountID, ChatID, ReturnKey         string
	OwnerID, FromChat, FromThread, ExpiresAt int64
}
type GroupSettings struct {
	OwnerID, Management    int64
	CommandTTL, ControlTTL int
}
type GroupStore interface {
	PrepareGroupControl(context.Context, delivery.Job, string, GroupSettings) (Plan, error)
	PrepareTopicHeader(context.Context, delivery.Job, string, GroupSettings) (Plan, error)
	TopicHeaderCurrent(context.Context, delivery.Job) (bool, error)
	CancelTopicHeader(context.Context, delivery.Job) error
}
type GroupOptions struct {
	OwnerID                int64
	CommandTTL, ControlTTL time.Duration
}
type Groups struct {
	store   GroupStore
	replies *Service
	master  interface {
		Me(context.Context) (*models.User, error)
	}
	settings GroupSettings
}

func NewGroups(store GroupStore, replies *Service, master interface {
	Me(context.Context) (*models.User, error)
}, c GroupOptions) (*Groups, error) {
	if c.CommandTTL == 0 {
		c.CommandTTL = time.Minute
	}
	if c.ControlTTL == 0 {
		c.ControlTTL = 10 * time.Minute
	}
	if store == nil || replies == nil || master == nil || c.OwnerID <= 0 || c.CommandTTL < 5*time.Second || c.CommandTTL > 24*time.Hour || c.CommandTTL%time.Second != 0 || c.ControlTTL < 5*time.Second || c.ControlTTL > 24*time.Hour || c.ControlTTL%time.Second != 0 {
		return nil, delivery.ErrInvalid
	}
	return &Groups{store: store, replies: replies, master: master, settings: GroupSettings{OwnerID: c.OwnerID, Management: replies.management, CommandTTL: int(c.CommandTTL / time.Second), ControlTTL: int(c.ControlTTL / time.Second)}}, nil
}
func (g *Groups) username(ctx context.Context) (string, error) {
	u, err := g.master.Me(ctx)
	if err != nil {
		return "", err
	}
	if u == nil || u.ID <= 0 || !u.IsBot || u.Username == "" {
		return "", providers.InvalidResponse("telegram", false)
	}
	return u.Username, nil
}
func (g *Groups) Handle(ctx context.Context, j delivery.Job, e *delivery.Effects, botID string, u *models.Update) error {
	if j.Kind != "tg" || u == nil {
		return delivery.ErrInvalid
	}
	if edited := u.EditedMessage; edited != nil {
		if p, ok := ParseCommand(edited); ok && (p.Name == "group" || (p.Name == "start" && len(p.Args) > 0 && strings.HasPrefix(p.Args[0], "mirror_"))) {
			return nil
		}
		return delivery.ErrUnsupported
	}
	parsed, command := ParseCommand(u.Message)
	callback := false
	if q := u.CallbackQuery; q != nil {
		parts := strings.Split(q.Data, ":")
		callback = len(parts) == 2 && topicOperation(parts[0])
		if !callback {
			return delivery.ErrUnsupported
		}
		if q.From.ID != g.settings.OwnerID || q.From.IsBot {
			return nil
		}
	}
	shared := u.Message != nil && u.Message.ChatShared != nil
	start := command && parsed.Name == "start" && len(parsed.Args) > 0 && mirrorStart.MatchString(parsed.Args[0]) && u.Message.Chat.Type == models.ChatTypePrivate && botID == "master"
	if !callback && !shared && !start && (!command || parsed.Name != "group") {
		return delivery.ErrUnsupported
	}
	if !callback {
		m := u.Message
		if m.From == nil || m.From.IsBot || m.From.ID != g.settings.OwnerID {
			return nil
		}
		if m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != g.settings.OwnerID {
			return delivery.ErrInvalid
		}
		if shared && (botID != "master" || m.Chat.Type != models.ChatTypePrivate) {
			return nil
		}
	}
	if saved, known, err := g.replies.Prepared(ctx, j, j.Dedup); err != nil {
		return err
	} else if known {
		return g.replies.Send(ctx, j, e, saved)
	}
	if command {
		if parsed.Recipient == "" && u.Message.Chat.Type != models.ChatTypePrivate && botID != "master" {
			return nil
		}
		addressed, err := g.replies.Addressed(ctx, j, botID, parsed.Recipient)
		if err != nil || !addressed {
			return err
		}
	}
	username := ""
	var err error
	if command && parsed.Name == "group" && botID == "master" {
		context, contextErr := g.replies.Context(ctx, j, botID, u.Message)
		if contextErr != nil {
			return contextErr
		}
		if context.Known && strings.HasSuffix(context.Route.ChatID, "@g.us") {
			username, err = g.username(ctx)
			if err != nil {
				return err
			}
		}
	}
	p, err := g.store.PrepareGroupControl(ctx, j, username, g.settings)
	if err != nil {
		return local(err)
	}
	return g.replies.Send(ctx, j, e, p)
}
func topicOperation(op string) bool {
	return op == "create" || op == "keep" || op == "mute" || op == "ignore" || op == "read" || op == "history"
}

func PrivateGroupKeyboard(id int32) *models.ReplyKeyboardMarkup {
	rights := &models.ChatAdministratorRights{CanManageChat: true, CanDeleteMessages: true, CanChangeInfo: true, CanInviteUsers: true}
	return &models.ReplyKeyboardMarkup{Keyboard: [][]models.KeyboardButton{{{Text: "Create / select Telegram group", RequestChat: &models.KeyboardButtonRequestChat{RequestID: id, ChatIsChannel: false, ChatHasUsername: false, ChatIsCreated: true, BotIsMember: true, UserAdministratorRights: rights, BotAdministratorRights: rights, RequestTitle: true}}}}, ResizeKeyboard: true, OneTimeKeyboard: true}
}
func (g *Groups) Header(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	p, known, err := g.replies.Prepared(ctx, j, j.Dedup)
	if err != nil {
		return err
	}
	if !known {
		username, err := g.username(ctx)
		if err != nil {
			return err
		}
		p, err = g.store.PrepareTopicHeader(ctx, j, username, g.settings)
		if err != nil {
			return local(err)
		}
	}
	current, err := g.store.TopicHeaderCurrent(ctx, j)
	if err != nil {
		return local(err)
	}
	if !current {
		return local(g.store.CancelTopicHeader(ctx, j))
	}
	return g.replies.Send(ctx, j, e, p)
}
