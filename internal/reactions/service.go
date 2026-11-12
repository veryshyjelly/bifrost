package reactions

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func buildPlan(key string, input Input, byDestination map[string]map[string]string, ttl int, now int64) Plan {
	p := Plan{Key: key, Input: input, NoticeTTL: ttl}
	for _, destination := range input.Destinations {
		desired := Desired(destination, input.State, byDestination[destination.Key])
		cache := input.Caches[destination.Key]
		// A member bot that cannot see this mapped message uses the master for the
		// same refusal signature for one minute, matching the source-side cache.
		for id, choice := range desired {
			refused, found := cache.Refused[id]
			if destination.Target.ChatID < 0 && id != "master" && id != destination.Target.BotID && found && refused.Reason == "message_missing" && refused.MessageID == destination.MessageID && refused.Emoji == TelegramEmoji(choice.Emoji) && now-refused.At < 60000 {
				delete(desired, id)
				choice.BotID = "master"
				if prior, exists := desired["master"]; !exists || prior.Order < choice.Order {
					desired["master"] = choice
				}
			}
		}
		p.Groups = append(p.Groups, Group{Destination: destination, Desired: desired})
		candidates := map[string]bool{}
		for id := range desired {
			candidates[id] = true
		}
		for id := range cache.Applied {
			candidates[id] = true
		}
		for id := range cache.Refused {
			candidates[id] = true
		}
		bots := make([]string, 0, len(candidates))
		for id := range candidates {
			bots = append(bots, id)
		}
		sort.Strings(bots)
		for _, id := range bots {
			choice := desired[id]
			emoji := TelegramEmoji(choice.Emoji)
			applied, exists := cache.Applied[id]
			if emoji == "" && (!exists || applied.Emoji == "") {
				continue
			}
			if exists && applied.MessageID == destination.MessageID && applied.Emoji == emoji {
				continue
			}
			if refused, exists := cache.Refused[id]; exists && refused.MessageID == destination.MessageID && refused.Emoji == emoji && now-refused.At < 60000 {
				continue
			}
			target := destination.Target
			target.BotID = id
			step := Step{Kind: "set", DestinationKey: destination.Key, BotID: id, Emoji: emoji, Actor: choice.Actor, Order: choice.Order, Target: target, MessageID: destination.MessageID, State: "pending"}
			if exists {
				prior := applied
				step.Prior = &prior
			}
			p.Steps = append(p.Steps, step)
		}
	}
	return p
}

func (s *Service) Incoming(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "wa" || e == nil {
		return delivery.ErrInvalid
	}
	var hook sdk.WebhookDelivery
	if json.Unmarshal(j.Payload, &hook) != nil {
		return delivery.ErrInvalid
	}
	if hook.Event != sdk.EventMessageReaction {
		return delivery.ErrUnsupported
	}
	if err := s.store.NormalizeChange(ctx, j, s.c.Session); err != nil {
		return local(err)
	}
	p, found, err := s.store.ReactionPlan(ctx, j.AccountID, j.Dedup)
	if err != nil {
		return local(err)
	}
	if !found {
		input, err := s.store.ReactionInput(ctx, j, s.c)
		if err != nil {
			return local(err)
		}
		choices := map[string]map[string]string{}
		for _, destination := range input.Destinations {
			choices[destination.Key] = map[string]string{}
			for actor := range input.State.Actors {
				if destination.Target.ChatID > 0 {
					choices[destination.Key][actor] = destination.Target.BotID
					continue
				}
				if err := e.Permit(); err != nil {
					return err
				}
				route := conversation.Route{ChatID: input.Source.ChatID, TelegramChatID: destination.Target.ChatID, ThreadID: destination.Target.ThreadID, Mode: conversation.Group}
				target, err := s.routing.SelectTarget(ctx, j.AccountID, route, actor, conversation.Flags{FromMe: input.SelfID != "" && actor == input.SelfID})
				if err != nil {
					return err
				}
				choices[destination.Key][actor] = target.BotID
			}
		}
		p = buildPlan(j.Dedup, input, choices, s.c.NoticeTTL, time.Now().UnixMilli())
		p, err = s.store.PrepareReactions(ctx, j, p, s.c)
		if err != nil {
			return local(err)
		}
	}
	for !p.Complete {
		index := -1
		for i, step := range p.Steps {
			if step.State == "pending" {
				index = i
				break
			}
		}
		if index < 0 {
			return local(s.store.CompleteReactions(ctx, j))
		}
		step := p.Steps[index]
		if step.Kind == "notice" {
			if err := s.store.AdvanceReaction(ctx, j, index); err != nil {
				return local(err)
			}
		} else {
			if err := e.Permit(); err != nil {
				return err
			}
			client, err := s.clients(ctx, j.AccountID, step.BotID)
			if err != nil {
				return local(err)
			}
			selection := []models.ReactionType{}
			if step.Emoji != "" {
				selection = append(selection, models.ReactionType{Type: models.ReactionTypeTypeEmoji, ReactionTypeEmoji: &models.ReactionTypeEmoji{Emoji: step.Emoji}})
			}
			err = client.SetReaction(ctx, &bot.SetMessageReactionParams{ChatID: step.Target.ChatID, MessageID: int(step.MessageID), Reaction: selection})
			if err != nil {
				var refusal *providers.Error
				if !errors.As(err, &refusal) || refusal.Retry || refusal.Uncertain || (refusal.Status != 400 && refusal.Status != 403) {
					return err
				}
				if err := s.store.RefuseReaction(ctx, j, index, refusal.Status, refusal.Reason); err != nil {
					return local(err)
				}
			} else if err := s.store.AdvanceReaction(ctx, j, index); err != nil {
				return local(err)
			}
		}
		var exists bool
		p, exists, err = s.store.ReactionPlan(ctx, j.AccountID, j.Dedup)
		if err != nil {
			return local(err)
		}
		if !exists {
			return delivery.ErrInvalid
		}
	}
	return nil
}

func reactionIdentity(value models.ReactionType) string {
	switch value.Type {
	case models.ReactionTypeTypeEmoji:
		if value.ReactionTypeEmoji != nil {
			return "emoji:" + value.ReactionTypeEmoji.Emoji
		}
	case models.ReactionTypeTypeCustomEmoji:
		if value.ReactionTypeCustomEmoji != nil {
			return "custom:" + value.ReactionTypeCustomEmoji.CustomEmojiID
		}
	}
	return string(value.Type)
}
func Selected(update models.MessageReactionUpdated) *models.ReactionType {
	old := map[string]bool{}
	for _, value := range update.OldReaction {
		old[reactionIdentity(value)] = true
	}
	var selected *models.ReactionType
	for i := range update.NewReaction {
		if !old[reactionIdentity(update.NewReaction[i])] {
			selected = &update.NewReaction[i]
		}
	}
	if selected == nil && len(update.NewReaction) > 0 {
		selected = &update.NewReaction[len(update.NewReaction)-1]
	}
	return selected
}

func (s *Service) Outgoing(ctx context.Context, j delivery.Job, e *delivery.Effects, _ string, u *models.Update) error {
	if j.Kind != "tg" || e == nil || u == nil {
		return delivery.ErrInvalid
	}
	if u.MessageReaction == nil {
		return delivery.ErrUnsupported
	}
	if u.MessageReaction.User == nil || u.MessageReaction.User.IsBot || u.MessageReaction.User.ID != s.c.OwnerID {
		return nil
	}
	p, found, err := s.store.OutgoingReactionPlan(ctx, j.AccountID, j.Dedup)
	if err != nil {
		return local(err)
	}
	if !found {
		input, err := s.store.OutgoingReactionInput(ctx, j, s.c)
		if err != nil {
			return local(err)
		}
		if !input.Mapped {
			return nil
		}
		p = OutgoingPlan{Key: j.Dedup, Session: s.c.Session, Input: input, Supported: true, State: "pending", NoticeTTL: s.c.NoticeTTL}
		if input.FrozenChoice != nil {
			p.Emoji = input.FrozenChoice.Emoji
			p.Supported = input.FrozenChoice.Supported
		} else if selected := Selected(input.Update); selected != nil {
			switch selected.Type {
			case models.ReactionTypeTypeEmoji:
				if selected.ReactionTypeEmoji == nil || !EmojiEquivalent(selected.ReactionTypeEmoji.Emoji) {
					p.Supported = false
				} else {
					p.Emoji = selected.ReactionTypeEmoji.Emoji
				}
			case models.ReactionTypeTypeCustomEmoji:
				if selected.ReactionTypeCustomEmoji == nil {
					p.Supported = false
					break
				}
				p.CustomID = selected.ReactionTypeCustomEmoji.CustomEmojiID
				emoji, known, err := s.store.CustomReactionEmoji(ctx, j.AccountID, p.CustomID)
				if err != nil {
					return local(err)
				}
				if !known {
					if err := e.Permit(); err != nil {
						return err
					}
					client, err := s.clients(ctx, j.AccountID, input.BotID)
					if err != nil {
						return local(err)
					}
					stickers, err := client.CustomEmoji(ctx, p.CustomID)
					if err != nil {
						var refusal *providers.Error
						if !errors.As(err, &refusal) || refusal.Retry || refusal.Uncertain || (refusal.Status != 400 && refusal.Status != 404) {
							return err
						}
					}
					for _, sticker := range stickers {
						if sticker != nil && sticker.CustomEmojiID == p.CustomID && EmojiEquivalent(sticker.Emoji) {
							emoji = sticker.Emoji
							break
						}
					}
				}
				p.Emoji = emoji
				p.Supported = EmojiEquivalent(emoji)
			default:
				p.Supported = false
			}
		}
		p, err = s.store.PrepareOutgoingReaction(ctx, j, p, s.c)
		if err != nil {
			return local(err)
		}
	}
	if p.State == "done" || p.State == "unsupported" {
		return nil
	}
	if !p.Supported {
		return delivery.ErrInvalid
	}
	if err := e.Permit(); err != nil {
		return err
	}
	if err := s.wa.React(ctx, p.Session, sdk.ReactMessageRequest{ChatID: p.Input.ChatID, MessageID: p.Input.MessageID, Emoji: p.Emoji}); err != nil {
		return err
	}
	return local(s.store.CompleteOutgoingReaction(ctx, j))
}
