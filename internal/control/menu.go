package control

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

type MenuPlan struct {
	BotID                     string
	Identity                  models.User
	OwnerID, ManagementChatID int64
	Commands, Scoped          []models.BotCommand
	Done                      int
}

func (p MenuPlan) Steps() int {
	if p.BotID == "master" {
		return 3
	}
	return 2
}

// Commands preserves the ordered legacy menus, including entries whose full
// workflows are still pending. Only fully composed runtimes may install them.
func Commands(master, management bool) []models.BotCommand {
	rows := [][2]string{{"start", "Show bridge help"}, {"sync", "Refresh WhatsApp metadata"}, {"history", "Import messages here: /history <n>"}, {"mention", "Find a WhatsApp group member and copy their tag"}, {"status", "Show bridge and queue status"}, {"clear", "Clear bot notices in this chat or topic"}, {"clear_log", "Clear bot notices in this chat or topic"}}
	if master {
		rows = append(rows, [][2]string{{"statuses", "Open and refresh WhatsApp Status posts"}, {"mute_status", "Reply to a Status post to mute its author"}, {"unmute_status", "Reply to a Status post to unmute its author"}, {"muted_statuses", "List and unmute people in the Status feed"}, {"create", "Reply to a participant to create their bot"}, {"group", "Create or select a Telegram mirror group"}, {"unmirror", "Move this mirror back to a management topic"}, {"read", "Mark this WhatsApp conversation read"}, {"keep", "Resume this conversation"}, {"mute", "Mute this conversation; reply in Status to mute its author"}, {"ignore", "Ignore this conversation"}, {"delete", "Delete a replied-to WhatsApp message"}, {"react", "React to a replied-to WhatsApp message"}}...)
	}
	if master && management {
		rows = append(rows, [][2]string{{"inbox", "Check the management forum"}, {"link", "Link a WhatsApp group manually"}, {"clear_all", "Clear messages in the current management topic"}}...)
	}
	out := make([]models.BotCommand, len(rows))
	for i, r := range rows {
		out[i] = models.BotCommand{Command: r[0], Description: r[1]}
	}
	return out
}

type MenuStore interface {
	MenuPlan(context.Context, delivery.Job) (MenuPlan, bool, error)
	PrepareMenu(context.Context, delivery.Job, MenuPlan) (MenuPlan, error)
	AdvanceMenu(context.Context, delivery.Job, int) error
}
type MenuTelegram interface {
	Me(context.Context) (*models.User, error)
	SetMyCommands(context.Context, *bot.SetMyCommandsParams) error
	SetChatMenuButton(context.Context, *bot.SetChatMenuButtonParams) error
}
type MenuClients func(context.Context, string, string) (MenuTelegram, error)
type Menus struct {
	store             MenuStore
	clients           MenuClients
	owner, management int64
}

func NewMenus(store MenuStore, clients MenuClients, owner, management int64) (*Menus, error) {
	if store == nil || clients == nil || owner <= 0 || management >= 0 {
		return nil, delivery.ErrInvalid
	}
	return &Menus{store: store, clients: clients, owner: owner, management: management}, nil
}

func (m *Menus) Install(ctx context.Context, job delivery.Job, e *delivery.Effects) error {
	var source struct {
		Bot string `json:"bot"`
	}
	if e == nil || job.Kind != "control-menu" || json.Unmarshal(job.Payload, &source) != nil || source.Bot == "" {
		return delivery.ErrInvalid
	}
	p, known, err := m.store.MenuPlan(ctx, job)
	if err != nil {
		return local(err)
	}
	if known && p.Done == p.Steps() {
		return nil
	}
	if err := e.Permit(); err != nil {
		return err
	}
	client, err := m.clients(ctx, job.AccountID, source.Bot)
	if err != nil {
		return err
	}
	if !known {
		if err := e.Permit(); err != nil {
			return err
		}
		identity, err := client.Me(ctx)
		if err != nil {
			return err
		}
		if identity == nil || !identity.IsBot || identity.ID <= 0 || identity.Username == "" || (source.Bot != "master" && strconv.FormatInt(identity.ID, 10) != source.Bot) {
			return providers.InvalidResponse("telegram", false)
		}
		p = MenuPlan{BotID: source.Bot, Identity: *identity, OwnerID: m.owner, ManagementChatID: m.management, Commands: Commands(source.Bot == "master", false)}
		if source.Bot == "master" {
			p.Scoped = Commands(true, true)
		}
		p, err = m.store.PrepareMenu(ctx, job, p)
		if err != nil {
			return local(err)
		}
	}
	for p.Done < p.Steps() {
		if err := e.Permit(); err != nil {
			return err
		}
		switch p.Done {
		case 0:
			err = client.SetMyCommands(ctx, &bot.SetMyCommandsParams{Commands: p.Commands, Scope: &models.BotCommandScopeDefault{}})
		case 1:
			err = client.SetChatMenuButton(ctx, &bot.SetChatMenuButtonParams{MenuButton: &models.MenuButtonCommands{Type: models.MenuButtonTypeCommands}})
		case 2:
			err = client.SetMyCommands(ctx, &bot.SetMyCommandsParams{Commands: p.Scoped, Scope: &models.BotCommandScopeChatMember{ChatID: p.ManagementChatID, UserID: p.OwnerID}})
		}
		if err != nil {
			return err
		}
		if err := m.store.AdvanceMenu(ctx, job, p.Done); err != nil {
			return local(err)
		}
		p.Done++
	}
	return nil
}
