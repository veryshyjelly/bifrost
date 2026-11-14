package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/reactions"
)

func validReactionSettings(c reactions.Settings) bool {
	return c.Session != "" && c.OwnerID > 0 && c.NoticeTTL >= 5 && c.NoticeTTL <= 86400
}
func reactionPlan(ctx context.Context, q *db.Queries, account, key string) (db.ReactionPlan, reactions.Plan, error) {
	row, err := q.ReactionPlan(ctx, db.ReactionPlanParams{AccountID: account, PlanKey: key})
	if err != nil {
		return row, reactions.Plan{}, err
	}
	var p reactions.Plan
	if json.Unmarshal(row.Payload, &p) != nil || p.Key != key {
		return row, p, delivery.ErrInvalid
	}
	p.Complete = row.Complete
	return row, p, nil
}
func (s *Store) ReactionPlan(ctx context.Context, account, key string) (reactions.Plan, bool, error) {
	_, p, err := reactionPlan(ctx, s.queries, account, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, false, nil
	}
	return p, err == nil, err
}
func saveReactions(ctx context.Context, q *db.Queries, row db.Job, p reactions.Plan) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return q.UpdateReactionPlan(ctx, db.UpdateReactionPlanParams{AccountID: row.AccountID, PlanKey: p.Key, Payload: raw})
}
func reactionCache(ctx context.Context, q *db.Queries, account, chat, message, destination string) (reactions.Cache, error) {
	raw, err := q.ReactionCache(ctx, db.ReactionCacheParams{AccountID: account, WhatsappChat: chat, MessageID: message, DestinationKey: destination})
	cache := reactions.Cache{Applied: map[string]reactions.Applied{}, Refused: map[string]reactions.Refused{}}
	if errors.Is(err, pgx.ErrNoRows) {
		return cache, nil
	}
	if err != nil {
		return cache, err
	}
	if json.Unmarshal(raw, &cache) != nil {
		return cache, delivery.ErrInvalid
	}
	if cache.Applied == nil {
		cache.Applied = map[string]reactions.Applied{}
	}
	if cache.Refused == nil {
		cache.Refused = map[string]reactions.Refused{}
	}
	return cache, nil
}
func saveReactionCache(ctx context.Context, q *db.Queries, row db.Job, source reactions.Event, key string, cache reactions.Cache) error {
	raw, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return q.SaveReactionCache(ctx, db.SaveReactionCacheParams{AccountID: row.AccountID, WhatsappChat: source.ChatID, MessageID: source.MessageID, DestinationKey: key, Payload: raw})
}

func reactionInput(ctx context.Context, q *db.Queries, row db.Job, c reactions.Settings) (reactions.Input, error) {
	result := reactions.Input{Caches: map[string]reactions.Cache{}, Bindings: map[string]string{}}
	var hook struct {
		Event   sdk.WebhookEvent `json:"event"`
		Account string           `json:"accountId"`
		Session string           `json:"sessionId"`
		Key     string           `json:"idempotencyKey"`
		Data    struct {
			ChatID    string          `json:"chatId"`
			MessageID string          `json:"messageId"`
			ID        string          `json:"id"`
			SenderID  string          `json:"senderId"`
			Reaction  string          `json:"reaction"`
			Reactions json.RawMessage `json:"reactions"`
		} `json:"data"`
	}
	if row.Kind != "wa" || !validReactionSettings(c) || json.Unmarshal(row.Payload, &hook) != nil || hook.Event != sdk.EventMessageReaction || hook.Account != row.AccountID || hook.Session != c.Session || hook.Key == "" || row.Dedup != "ow:"+hook.Key || hook.Data.ChatID == "" {
		return result, delivery.ErrInvalid
	}
	id := hook.Data.MessageID
	if id == "" {
		id = hook.Data.ID
	}
	if id == "" {
		return result, delivery.ErrInvalid
	}
	d, err := directory(ctx, q, row.AccountID)
	if err != nil {
		return result, err
	}
	chat, err := conversationKey(ctx, q, row.AccountID, hook.Data.ChatID, d)
	if err != nil {
		return result, err
	}
	if row.Stream != "wa:"+chat {
		return result, delivery.ErrInvalid
	}
	sender := hook.Data.SenderID
	if sender == "" {
		sender = "unknown"
	}
	sender = d.Canonical(sender)
	snapshot, err := reactions.Snapshot(hook.Data.Reactions)
	if err != nil {
		return result, delivery.ErrInvalid
	}
	if snapshot != nil {
		for i := range *snapshot {
			(*snapshot)[i].Actor = d.Canonical((*snapshot)[i].Actor)
		}
	}
	source := reactions.Event{Session: c.Session, ChatID: chat, MessageID: id, SenderID: sender, Reaction: hook.Data.Reaction, Snapshot: snapshot}
	result.Source = source
	result.SenderName = d.SenderName(sender)
	revoked, err := q.HistoryMessageRevoked(ctx, db.HistoryMessageRevokedParams{AccountID: row.AccountID, WhatsappChat: chat, MessageID: id})
	if err != nil {
		return result, err
	}
	if revoked {
		return result, nil
	}
	self, err := q.SelfIdentity(ctx, row.AccountID)
	if err == nil {
		result.SelfID = d.Canonical(self)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	var previous reactions.State
	raw, err := q.ReactionState(ctx, db.ReactionStateParams{AccountID: row.AccountID, WhatsappChat: chat, MessageID: id})
	if err == nil {
		if json.Unmarshal(raw, &previous) != nil {
			return result, delivery.ErrInvalid
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	var echo *reactions.Echo
	raw, err = q.ReactionEcho(ctx, db.ReactionEchoParams{AccountID: row.AccountID, WhatsappChat: chat, MessageID: id})
	if err == nil {
		var value reactions.Echo
		if json.Unmarshal(raw, &value) != nil {
			return result, delivery.ErrInvalid
		}
		value.ActorID = d.Canonical(value.ActorID)
		if value.ActorID == "" {
			value.ActorID = result.SelfID
		}
		if value.ActorID != sender && identity.IsPerson(sender) {
			own, err := q.SourceActorIsOwn(ctx, db.SourceActorIsOwnParams{AccountID: row.AccountID, Identities: d.Aliases(sender)})
			if err != nil {
				return result, err
			}
			if own.Bool {
				value.ActorID = sender
			}
		}
		echo = &value
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	result.State = reactions.Reduce(previous, source, row.Dedup, echo, time.Now().UnixMilli())
	raw, err = json.Marshal(result.State)
	if err != nil {
		return result, err
	}
	if err := q.SaveReactionState(ctx, db.SaveReactionStateParams{AccountID: row.AccountID, WhatsappChat: chat, MessageID: id, Payload: raw}); err != nil {
		return result, err
	}
	for actor := range result.State.Actors {
		person := d.Person(actor, nil)
		if person.Registered {
			result.Bindings[actor] = person.Bot.ID
		} else {
			result.Bindings[actor] = ""
		}
	}
	mappings, err := q.RetainedSourceMappings(ctx, db.RetainedSourceMappingsParams{AccountID: row.AccountID, WhatsappChat: chat, WhatsappMessageID: id})
	if err != nil {
		return result, err
	}
	type candidate struct {
		mapping db.RetainedSourceMappingsRow
		media   bool
	}
	grouped := map[string][]candidate{}
	for _, mapping := range mappings {
		var receipt models.Message
		if json.Unmarshal(mapping.Receipt, &receipt) != nil {
			return result, delivery.ErrInvalid
		}
		media := mapping.Media || len(receipt.Photo) > 0 || receipt.Video != nil || receipt.Animation != nil || receipt.Audio != nil || receipt.Voice != nil || receipt.Document != nil || receipt.Sticker != nil
		var plan struct {
			Outgoing *struct{ Asset json.RawMessage }
		}
		if json.Unmarshal(mapping.PlanPayload, &plan) == nil && plan.Outgoing != nil && len(plan.Outgoing.Asset) > 0 && string(plan.Outgoing.Asset) != "null" {
			media = true
		}
		key := fmt.Sprintf("%s:%d:%d", relayScope(mapping.BotID, mapping.TelegramChatID), mapping.TelegramChatID, mapping.ThreadID)
		grouped[key] = append(grouped[key], candidate{mapping: mapping, media: media})
	}
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		candidates := grouped[key]
		selected := candidates[len(candidates)-1]
		// Retain the freshest delivery plan, preferring its attachment over labels.
		for i := len(candidates) - 1; i >= 0; i-- {
			// Baseline copies did not have plan groups; their last physical row
			// is the original reaction target, including a later text-only copy.
			if !selected.mapping.PlanKey.Valid {
				break
			}
			if candidates[i].mapping.PlanKey != selected.mapping.PlanKey {
				break
			}
			if candidates[i].media {
				selected = candidates[i]
				break
			}
		}
		m := selected.mapping
		destination := reactions.Destination{Key: key, Target: control.Target{BotID: m.BotID, ChatID: m.TelegramChatID, ThreadID: m.ThreadID}, MessageID: m.TelegramMessageID}
		result.Destinations = append(result.Destinations, destination)
		cache, err := reactionCache(ctx, q, row.AccountID, chat, id, key)
		if err != nil {
			return result, err
		}
		result.Caches[key] = cache
	}
	return result, nil
}
func (s *Store) ReactionInput(ctx context.Context, j delivery.Job, c reactions.Settings) (reactions.Input, error) {
	var result reactions.Input
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		var err error
		result, err = reactionInput(ctx, q, row, c)
		return err
	})
	return result, err
}
func (s *Store) PrepareReactions(ctx context.Context, j delivery.Job, p reactions.Plan, c reactions.Settings) (reactions.Plan, error) {
	var result reactions.Plan
	if p.Key != j.Dedup || p.NoticeTTL != c.NoticeTTL || !validReactionSettings(c) || p.Complete {
		return result, delivery.ErrInvalid
	}
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if owner, old, err := reactionPlan(ctx, q, row.AccountID, p.Key); err == nil {
			if owner.JobID != row.ID {
				return delivery.ErrInvalid
			}
			result = old
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		actual, err := reactionInput(ctx, q, row, c)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, p.Input) {
			return delivery.ErrLocalRetry
		}
		if len(p.Groups) != len(actual.Destinations) {
			return delivery.ErrInvalid
		}
		groups := map[string]reactions.Group{}
		for i, group := range p.Groups {
			if group.Destination != actual.Destinations[i] || group.Unavailable {
				return delivery.ErrInvalid
			}
			for bot, choice := range group.Desired {
				actor, exists := actual.State.Actors[choice.Actor]
				if !exists || choice.Order != actor.Order || choice.Emoji != actor.Emoji || choice.BotID != bot || (group.Destination.Target.ChatID < 0 && bot != "master" && bot != actual.Bindings[choice.Actor]) || (group.Destination.Target.ChatID > 0 && bot != group.Destination.Target.BotID) {
					return delivery.ErrInvalid
				}
			}
			groups[group.Destination.Key] = group
		}
		for _, step := range p.Steps {
			group, exists := groups[step.DestinationKey]
			if !exists || (step.Kind != "set" && step.Kind != "forget-refusal") || step.State != "pending" || step.ClearRefusal || step.BotID != step.Target.BotID || step.Target.ChatID != group.Destination.Target.ChatID || step.Target.ThreadID != group.Destination.Target.ThreadID || step.MessageID != group.Destination.MessageID {
				return delivery.ErrInvalid
			}
			if step.Kind == "forget-refusal" {
				cache := actual.Caches[step.DestinationKey]
				_, refused := cache.Refused[step.BotID]
				if _, wanted := group.Desired[step.BotID]; wanted || !refused || cache.Applied[step.BotID].Emoji != "" || step.Emoji != "" || step.Actor != "" || step.Order != 0 || step.Prior != nil {
					return delivery.ErrInvalid
				}
				continue
			}
			desired, exists := group.Desired[step.BotID]
			if exists {
				if step.Emoji != reactions.TelegramEmoji(desired.Emoji) || step.Actor != desired.Actor || step.Order != desired.Order {
					return delivery.ErrInvalid
				}
			} else {
				if step.Emoji != "" {
					return delivery.ErrInvalid
				}
				if _, exists := actual.Caches[step.DestinationKey].Applied[step.BotID]; !exists {
					return delivery.ErrInvalid
				}
			}
		}
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if err := q.PrepareReactionPlan(ctx, db.PrepareReactionPlanParams{AccountID: row.AccountID, PlanKey: p.Key, JobID: row.ID, Payload: raw}); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}
func reactionOwned(ctx context.Context, q *db.Queries, row db.Job, index int) (reactions.Plan, error) {
	owner, p, err := reactionPlan(ctx, q, row.AccountID, row.Dedup)
	if err != nil {
		return p, err
	}
	if owner.JobID != row.ID || index < 0 || index >= len(p.Steps) {
		return p, delivery.ErrInvalid
	}
	return p, nil
}
func (s *Store) AdvanceReaction(ctx context.Context, j delivery.Job, index int) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, err := reactionOwned(ctx, q, row, index)
		if err != nil {
			return err
		}
		step := &p.Steps[index]
		if step.State == "done" {
			return nil
		}
		if step.State != "pending" {
			return delivery.ErrInvalid
		}
		if step.Kind == "notice" {
			target := step.Target
			if target.ChatID < 0 {
				target.BotID = "master"
			}
			text := p.Input.SenderName + " removed a reaction · native reaction unavailable"
			if p.Input.Source.Reaction != "" {
				text = p.Input.SenderName + " reacted " + p.Input.Source.Reaction + " · native reaction unavailable"
			}
			n := control.Notice{Target: target, Body: presentation.Plain(text), ReplyTo: step.MessageID, TTLSeconds: p.NoticeTTL}
			if _, err := metadataEnqueue(ctx, q, row.AccountID, row.Dedup+":reaction-notice:"+step.DestinationKey, "notice:"+row.Dedup, "control-notice", n); err != nil {
				return err
			}
		} else if step.Kind == "forget-refusal" {
			cache, err := reactionCache(ctx, q, row.AccountID, p.Input.Source.ChatID, p.Input.Source.MessageID, step.DestinationKey)
			if err != nil {
				return err
			}
			delete(cache.Refused, step.BotID)
			if err := saveReactionCache(ctx, q, row, p.Input.Source, step.DestinationKey, cache); err != nil {
				return err
			}
		} else if step.Kind == "set" {
			cache, err := reactionCache(ctx, q, row.AccountID, p.Input.Source.ChatID, p.Input.Source.MessageID, step.DestinationKey)
			if err != nil {
				return err
			}
			cache.Applied[step.BotID] = reactions.Applied{MessageID: step.MessageID, Emoji: step.Emoji, Actor: step.Actor, Order: step.Order}
			if !step.ClearRefusal {
				delete(cache.Refused, step.BotID)
			}
			if err := saveReactionCache(ctx, q, row, p.Input.Source, step.DestinationKey, cache); err != nil {
				return err
			}
		} else {
			return delivery.ErrInvalid
		}
		step.State = "done"
		return saveReactions(ctx, q, row, p)
	})
}
func (s *Store) RefuseReaction(ctx context.Context, j delivery.Job, index, status int, reason string) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if status != 400 && status != 403 {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, err := reactionOwned(ctx, q, row, index)
		if err != nil {
			return err
		}
		step := p.Steps[index]
		if step.State == "refused" {
			return nil
		}
		if step.Kind != "set" || step.State != "pending" {
			return delivery.ErrInvalid
		}
		cache, err := reactionCache(ctx, q, row.AccountID, p.Input.Source.ChatID, p.Input.Source.MessageID, step.DestinationKey)
		if err != nil {
			return err
		}
		cache.Refused[step.BotID] = reactions.Refused{MessageID: step.MessageID, Emoji: step.Emoji, Reason: reason, At: time.Now().UnixMilli()}
		if err := saveReactionCache(ctx, q, row, p.Input.Source, step.DestinationKey, cache); err != nil {
			return err
		}
		p.Steps[index].State = "refused"
		if step.ClearRefusal {
			return saveReactions(ctx, q, row, p)
		}
		groupIndex := -1
		for i, group := range p.Groups {
			if group.Destination.Key == step.DestinationKey {
				groupIndex = i
				break
			}
		}
		if groupIndex < 0 {
			return delivery.ErrInvalid
		}
		group := &p.Groups[groupIndex]
		if reason == "message_missing" || reason == "thread_missing" {
			if reason == "message_missing" && step.Emoji != "" && step.Target.ChatID < 0 && step.BotID != "master" && step.BotID != group.Destination.Target.BotID {
				choice, exists := group.Desired[step.BotID]
				if exists {
					delete(group.Desired, step.BotID)
					if prior, exists := group.Desired["master"]; !exists || prior.Order < choice.Order {
						choice.BotID = "master"
						group.Desired["master"] = choice
						target := step.Target
						target.BotID = "master"
						p.Steps = append(p.Steps, reactions.Step{Kind: "set", DestinationKey: step.DestinationKey, BotID: "master", Emoji: step.Emoji, Actor: step.Actor, Order: step.Order, Target: target, MessageID: step.MessageID, State: "pending"})
					}
				}
			} else {
				group.Unavailable = true
				for i := range p.Steps {
					if p.Steps[i].DestinationKey == step.DestinationKey && p.Steps[i].State == "pending" {
						p.Steps[i].State = "skipped"
					}
				}
			}
		} else {
			if step.Prior != nil && step.Prior.Emoji != "" {
				clear := step
				clear.Emoji = ""
				clear.Actor = ""
				clear.Order = 0
				clear.MessageID = step.Prior.MessageID
				clear.Prior = nil
				clear.ClearRefusal = true
				clear.State = "pending"
				p.Steps = append(p.Steps, clear)
			}
			p.Steps = append(p.Steps, reactions.Step{Kind: "notice", DestinationKey: step.DestinationKey, Target: group.Destination.Target, MessageID: group.Destination.MessageID, State: "pending"})
		}
		return saveReactions(ctx, q, row, p)
	})
}
func (s *Store) CompleteReactions(ctx context.Context, j delivery.Job) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		owner, p, err := reactionPlan(ctx, q, row.AccountID, row.Dedup)
		if err != nil {
			return err
		}
		if owner.JobID != row.ID {
			return delivery.ErrInvalid
		}
		for _, step := range p.Steps {
			if step.State == "pending" {
				return delivery.ErrIncomplete
			}
		}
		return q.CompleteReactionPlan(ctx, db.CompleteReactionPlanParams{AccountID: row.AccountID, PlanKey: row.Dedup})
	})
}
