// Package conversation implements routing decisions and inbox creation. The
// caller supplies canonical identities; provider dispatch and alias verification
// are separate capabilities and must precede automatic live relaying.
package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

type Mode string

const (
	Inbox     Mode = "inbox"
	Dedicated Mode = "dedicated"
	Group     Mode = "group"
	Muted     Mode = "muted"
	Ignored   Mode = "ignored"
)

type Route struct {
	ChatID, Name             string
	TelegramChatID, ThreadID int64
	Mode                     Mode
	InboxRetired             bool
}
type Binding struct {
	BotID   string
	Started bool
}
type Target struct {
	BotID                                    string
	ChatID, ThreadID                         int64
	Suppressed, ShowSender, PhoneOriginOwner bool
}
type Flags struct{ FromMe, Status, ForceMaster bool }

var ErrInboxRequired = errors.New("restore the management inbox before delivering through master")

type Store interface {
	ConversationRoute(context.Context, string, string) (Route, bool, error)
	ContactBot(context.Context, string, string) (Binding, bool, error)
	CreateInbox(context.Context, delivery.Job, Route) (Route, error)
}
type Telegram interface {
	CreateForumTopic(context.Context, *bot.CreateForumTopicParams) (*models.ForumTopic, error)
	ChatMember(context.Context, *bot.GetChatMemberParams) (*models.ChatMember, error)
}
type Service struct {
	store             Store
	telegram          Telegram
	management, owner int64
}

func New(store Store, telegram Telegram, management, owner int64) (*Service, error) {
	if store == nil || telegram == nil || management >= 0 || owner <= 0 {
		return nil, delivery.ErrInvalid
	}
	return &Service{store: store, telegram: telegram, management: management, owner: owner}, nil
}

func topicName(name, fallback string) string {
	if name == "" {
		name = fallback
	}
	units, end := 0, 0
	for i, r := range name {
		cost := 1
		if r > 0xffff {
			cost = 2
		}
		if units+cost > 128 {
			return name[:i]
		}
		units += cost
		end = i + len(string(r))
	}
	return name[:end]
}

// EnsureInbox is only for the initial WhatsApp conversation stream. Other route
// transitions must not create a competing first topic from a Telegram stream.
func (s *Service) EnsureInbox(ctx context.Context, job delivery.Job, effects *delivery.Effects, chat, name string) (Route, error) {
	if chat == "" || job.Kind != "wa" || job.Stream != "wa:"+chat || effects == nil {
		return Route{}, delivery.ErrInvalid
	}
	route, found, err := s.store.ConversationRoute(ctx, job.AccountID, chat)
	if err != nil {
		return Route{}, delivery.ErrLocalRetry
	}
	if found {
		return route, nil
	}
	binding, bound, err := s.store.ContactBot(ctx, job.AccountID, chat)
	if err != nil {
		if errors.Is(err, identity.ErrConflict) {
			return Route{}, err
		}
		return Route{}, delivery.ErrLocalRetry
	}
	route = Route{ChatID: chat, Name: name, TelegramChatID: s.management, Mode: Inbox}
	if !strings.HasSuffix(chat, "@g.us") && bound {
		route.Mode = Dedicated
		if binding.Started {
			route.InboxRetired = true
			return s.commitInbox(ctx, job, route)
		}
	}
	request, _ := json.Marshal(struct {
		ChatID int64  `json:"chat_id"`
		Name   string `json:"name"`
	}{s.management, topicName(name, chat)})
	receipt, err := effects.Do(job.Dedup+":topic", request, func(ctx context.Context, request json.RawMessage) (json.RawMessage, error) {
		var frozen struct {
			ChatID int64  `json:"chat_id"`
			Name   string `json:"name"`
		}
		if json.Unmarshal(request, &frozen) != nil || frozen.ChatID == 0 || frozen.Name == "" {
			return nil, delivery.ErrInvalid
		}
		topic, err := s.telegram.CreateForumTopic(ctx, &bot.CreateForumTopicParams{ChatID: frozen.ChatID, Name: frozen.Name})
		if err != nil {
			return nil, err
		}
		if topic == nil || topic.MessageThreadID <= 0 {
			return nil, providers.InvalidResponse("telegram", true)
		}
		return json.Marshal(topic)
	})
	if err != nil {
		return Route{}, err
	}
	var topic models.ForumTopic
	if json.Unmarshal(receipt, &topic) != nil || topic.MessageThreadID <= 0 {
		return Route{}, delivery.ErrUncertain
	}
	route.ThreadID = int64(topic.MessageThreadID)
	return s.commitInbox(ctx, job, route)
}
func (s *Service) commitInbox(ctx context.Context, job delivery.Job, route Route) (Route, error) {
	r, err := s.store.CreateInbox(ctx, job, route)
	if errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, delivery.ErrInvalid) {
		return Route{}, err
	}
	if err != nil {
		return Route{}, delivery.ErrLocalRetry
	}
	return r, nil
}

// SelectTarget performs only a Telegram membership read. A retired inbox is an
// explicit repair requirement, never permission to post into the whole forum.
func (s *Service) SelectTarget(ctx context.Context, account string, route Route, sender string, flags Flags) (Target, error) {
	if account == "" || route.ChatID == "" || route.TelegramChatID == 0 {
		return Target{}, delivery.ErrInvalid
	}
	switch route.Mode {
	case Inbox, Dedicated, Group, Muted, Ignored:
	default:
		return Target{}, delivery.ErrInvalid
	}
	if route.Mode == Muted || route.Mode == Ignored {
		return Target{Suppressed: true}, nil
	}
	private := route.Mode == Dedicated
	target := Target{BotID: "master", ChatID: route.TelegramChatID, ThreadID: route.ThreadID, ShowSender: true, PhoneOriginOwner: flags.FromMe}
	if !flags.ForceMaster && !flags.Status && !(flags.FromMe && !private) {
		binding, found, err := s.store.ContactBot(ctx, account, sender)
		if err != nil {
			if errors.Is(err, identity.ErrConflict) {
				return Target{}, err
			}
			return Target{}, delivery.ErrLocalRetry
		}
		if found {
			if private {
				if binding.Started {
					target.BotID = binding.BotID
					target.ChatID = s.owner
					target.ThreadID = 0
					target.ShowSender = flags.FromMe
				}
			} else {
				id, err := strconv.ParseInt(binding.BotID, 10, 64)
				if err != nil || id <= 0 {
					return Target{}, delivery.ErrInvalid
				}
				member, err := s.telegram.ChatMember(ctx, &bot.GetChatMemberParams{ChatID: route.TelegramChatID, UserID: id})
				var failure *providers.Error
				if err != nil {
					if !errors.As(err, &failure) || failure.Status != 400 {
						return Target{}, err
					}
				} else {
					if member == nil {
						return Target{}, providers.InvalidResponse("telegram", false)
					}
					switch member.Type {
					case models.ChatMemberTypeOwner, models.ChatMemberTypeAdministrator, models.ChatMemberTypeMember:
						target.BotID = binding.BotID
						target.ShowSender = false
					}
				}
			}
		}
	}
	if target.BotID == "master" && route.InboxRetired {
		return Target{}, ErrInboxRequired
	}
	return target, nil
}
