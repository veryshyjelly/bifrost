package relay

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

var ErrNativeRepairUnsafe = errors.New("native delivery cannot move after a receipt or uncertain send")

type NativeRepairStore interface {
	BeginNativeRepair(context.Context, delivery.Job, string, NativeRepair) (NativePlan, error)
	CommitNativeRepair(context.Context, delivery.Job, NativePlan) (NativePlan, error)
}
type nativeTopicCreator interface {
	CreateForumTopic(context.Context, *bot.CreateForumTopicParams) (*models.ForumTopic, error)
}

func (n *Native) repair(ctx context.Context, j delivery.Job, e *delivery.Effects, p NativePlan, refusal *providers.Error) (bool, error) {
	store, ok := n.store.(NativeRepairStore)
	if !ok || p.Origin != "live" || p.Repairs >= 2 || p.Content == nil {
		return false, nil
	}
	r := NativeRepair{Management: n.c.Management, Reason: refusal.Reason}
	switch {
	case refusal.Reason == "inbox_required" && p.BotID == "master" && p.Route.InboxRetired:
		r.Target = control.Target{BotID: "master", ChatID: n.c.Management}
		r.CreateTopic = true
	case refusal.Status == 403 && p.BotID != "master":
		r.Reason = "member_forbidden"
		r.Target = control.Target{BotID: "master", ChatID: p.Route.TelegramChatID, ThreadID: p.Route.ThreadID}
		r.CreateTopic = p.Route.InboxRetired
	case refusal.Status == 400 && refusal.Reason == "thread_missing" && p.BotID == "master" && p.TelegramChat == n.c.Management && p.ThreadID > 0 && p.Route.TelegramChatID == n.c.Management:
		r.Target = control.Target{BotID: "master", ChatID: n.c.Management}
		// A newly available private bot takes precedence over recreating an inbox.
		if p.Route.Mode == conversation.Dedicated && p.Repairs == 0 {
			target, err := n.routing.SelectTarget(ctx, j.AccountID, p.Route, p.Source.SenderID, conversation.Flags{FromMe: p.Source.Own})
			if err != nil && !errors.Is(err, conversation.ErrInboxRequired) {
				return false, err
			}
			if err == nil && target.BotID != "master" {
				r.Target = control.Target{BotID: target.BotID, ChatID: target.ChatID, ThreadID: target.ThreadID}
			}
		}
		r.CreateTopic = r.Target.BotID == "master"
	default:
		return false, nil
	}
	r.Prefix = j.Dedup + ":native:repair:" + strconv.Itoa(p.Repairs+1)
	if r.CreateTopic {
		r.Target = control.Target{BotID: "master", ChatID: n.c.Management}
		r.TopicKey, r.TopicName = r.Prefix+":topic", conversation.TopicName(p.Route.Name, p.WhatsAppChat)
	}
	prepared, err := store.BeginNativeRepair(ctx, j, p.Key, r)
	if errors.Is(err, ErrNativeRepairUnsafe) {
		return false, nil
	}
	if err != nil {
		return false, local(err)
	}
	return true, n.resumeRepair(ctx, j, e, prepared)
}

func (n *Native) resumeRepair(ctx context.Context, j delivery.Job, e *delivery.Effects, p NativePlan) error {
	store, ok := n.store.(NativeRepairStore)
	if !ok || p.Repair == nil {
		return delivery.ErrInvalid
	}
	r := *p.Repair
	if r.CreateTopic {
		request, _ := json.Marshal(bot.CreateForumTopicParams{ChatID: r.Management, Name: r.TopicName})
		receipt, err := e.Do(r.TopicKey, request, func(call context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var request bot.CreateForumTopicParams
			if json.Unmarshal(raw, &request) != nil {
				return nil, delivery.ErrInvalid
			}
			client, err := n.clients(call, j.AccountID, "master")
			if err != nil {
				return nil, local(err)
			}
			creator, ok := client.(nativeTopicCreator)
			if !ok {
				return nil, delivery.ErrUnsupported
			}
			topic, err := creator.CreateForumTopic(call, &request)
			if err != nil {
				return nil, err
			}
			if topic == nil || topic.MessageThreadID <= 1 {
				return nil, providers.InvalidResponse("telegram", true)
			}
			return json.Marshal(topic)
		})
		if err != nil {
			return err
		}
		var topic models.ForumTopic
		if json.Unmarshal(receipt, &topic) != nil || topic.MessageThreadID <= 1 {
			return delivery.ErrUncertain
		}
		r.Target.ThreadID = int64(topic.MessageThreadID)
		p.Route.ThreadID, p.Route.InboxRetired = r.Target.ThreadID, false
	}
	p.BotID, p.TelegramChat, p.ThreadID = r.Target.BotID, r.Target.ChatID, r.Target.ThreadID
	p.Prefix = r.Prefix
	p.Packets = nil
	choices := p.PollChoices
	p.PollChoices = nil
	if err := n.render(ctx, j, &p); err != nil {
		return err
	}
	if len(choices) > 0 {
		p.PollChoices = choices
		var buttons [][]models.InlineKeyboardButton
		for _, c := range choices {
			text := []rune(c.Value)
			if len(text) > 64 {
				text = text[:64]
			}
			buttons = append(buttons, []models.InlineKeyboardButton{{Text: string(text), CallbackData: "vote:" + c.ID}})
		}
		for i := range p.Packets {
			if p.Packets[i].Kind == "text" && !p.Packets[i].NoMapping {
				p.Packets[i].Markup = &models.InlineKeyboardMarkup{InlineKeyboard: buttons}
			}
		}
	}
	committed, err := store.CommitNativeRepair(ctx, j, p)
	if err != nil {
		return local(err)
	}
	return n.send(ctx, j, e, committed)
}
