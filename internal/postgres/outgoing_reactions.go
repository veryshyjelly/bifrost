package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/reactions"
	"github.com/veryshyjelly/bifrost/internal/retained"
)

func outgoingReactionPlan(ctx context.Context, q *db.Queries, account, key string) (db.OutgoingReactionPlan, reactions.OutgoingPlan, error) {
	row, err := q.OutgoingReactionPlan(ctx, db.OutgoingReactionPlanParams{AccountID: account, PlanKey: key})
	if err != nil {
		return row, reactions.OutgoingPlan{}, err
	}
	var p reactions.OutgoingPlan
	if json.Unmarshal(row.Payload, &p) != nil || p.Key != key {
		return row, p, delivery.ErrInvalid
	}
	return row, p, nil
}
func (s *Store) OutgoingReactionPlan(ctx context.Context, account, key string) (reactions.OutgoingPlan, bool, error) {
	_, p, err := outgoingReactionPlan(ctx, s.queries, account, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, false, nil
	}
	return p, err == nil, err
}
func outgoingReactionInput(ctx context.Context, q *db.Queries, row db.Job, c reactions.Settings) (reactions.OutgoingInput, error) {
	var result reactions.OutgoingInput
	var envelope struct {
		Bot    string          `json:"bot"`
		Update json.RawMessage `json:"update"`
	}
	var u models.Update
	if row.Kind != "tg" || !validReactionSettings(c) || json.Unmarshal(row.Payload, &envelope) != nil || json.Unmarshal(envelope.Update, &u) != nil || envelope.Bot == "" || u.MessageReaction == nil || row.Dedup != "tg:"+envelope.Bot+":"+strconv.FormatInt(u.ID, 10) || !ingress.AuthorizedUpdate(envelope.Update, &u, c.OwnerID) {
		return result, delivery.ErrInvalid
	}
	update := u.MessageReaction
	result.BotID = envelope.Bot
	result.Update = *update
	if update.User == nil || update.User.IsBot || update.User.ID != c.OwnerID || (update.Chat.ID < 0 && envelope.Bot != "master") {
		return result, nil
	}
	if update.Chat.Type == models.ChatTypePrivate && update.Chat.ID != c.OwnerID {
		return result, delivery.ErrInvalid
	}
	mapped, err := q.TelegramMapping(ctx, db.TelegramMappingParams{AccountID: row.AccountID, Scope: relayScope(envelope.Bot, update.Chat.ID), TelegramChatID: update.Chat.ID, TelegramMessageID: int64(update.MessageID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if mapped.Deleted || mapped.ObservationDeleted || mapped.WhatsappChat == "status@broadcast" {
		return result, nil
	}
	revoked, err := q.HistoryMessageRevoked(ctx, db.HistoryMessageRevokedParams{AccountID: row.AccountID, WhatsappChat: mapped.WhatsappChat, MessageID: mapped.WhatsappMessageID})
	if err != nil {
		return result, err
	}
	if revoked {
		return result, nil
	}
	if update.Chat.ID > 0 {
		contact, err := q.ContactForBot(ctx, db.ContactForBotParams{AccountID: row.AccountID, BotID: envelope.Bot})
		if errors.Is(err, pgx.ErrNoRows) {
			return result, nil
		}
		if err != nil {
			return result, err
		}
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return result, err
		}
		chat, err := conversationKey(ctx, q, row.AccountID, contact, d)
		if err != nil {
			return result, err
		}
		if chat != mapped.WhatsappChat {
			return result, nil
		}
	} else {
		route, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: mapped.WhatsappChat})
		if err != nil {
			return result, err
		}
		if route.TelegramChatID != update.Chat.ID || route.ThreadID != mapped.ThreadID {
			return result, nil
		}
	}
	result.Mapped = true
	result.ChatID = mapped.WhatsappChat
	result.MessageID = mapped.WhatsappMessageID
	result.TelegramMessageID = int64(update.MessageID)
	result.Target = control.Target{BotID: envelope.Bot, ChatID: update.Chat.ID, ThreadID: mapped.ThreadID}
	self, err := q.SelfIdentity(ctx, row.AccountID)
	if err == nil {
		result.SelfID = self
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	choice, err := q.RetainedReactionChoice(ctx, db.RetainedReactionChoiceParams{AccountID: row.AccountID, SourceKey: retained.OutgoingReactionKey(row.AccountID, envelope.Bot, row.Dedup)})
	if err == nil {
		result.FrozenChoice = &reactions.FrozenChoice{Emoji: choice.Emoji, Supported: choice.Supported}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	return result, nil
}
func (s *Store) OutgoingReactionInput(ctx context.Context, j delivery.Job, c reactions.Settings) (reactions.OutgoingInput, error) {
	var result reactions.OutgoingInput
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		var err error
		result, err = outgoingReactionInput(ctx, q, row, c)
		return err
	})
	return result, err
}
func (s *Store) CustomReactionEmoji(ctx context.Context, account, id string) (string, bool, error) {
	emoji, err := s.queries.CustomReactionEmoji(ctx, db.CustomReactionEmojiParams{AccountID: account, CustomID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err == nil && !reactions.EmojiEquivalent(emoji) {
		return "", false, nil
	}
	return emoji, err == nil, err
}
func saveOutgoingReaction(ctx context.Context, q *db.Queries, row db.Job, p reactions.OutgoingPlan) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return q.UpdateOutgoingReactionPlan(ctx, db.UpdateOutgoingReactionPlanParams{AccountID: row.AccountID, PlanKey: p.Key, Payload: raw})
}
func recordReactionEcho(ctx context.Context, q *db.Queries, row db.Job, p reactions.OutgoingPlan) error {
	echo := reactions.Echo{ActorID: p.Input.SelfID, Emoji: reactions.EmojiKey(p.Emoji), Target: p.Input.Target, MessageID: p.Input.TelegramMessageID, At: time.Now().UnixMilli()}
	raw, err := json.Marshal(echo)
	if err != nil {
		return err
	}
	return q.SaveReactionEcho(ctx, db.SaveReactionEchoParams{AccountID: row.AccountID, WhatsappChat: p.Input.ChatID, MessageID: p.Input.MessageID, Payload: raw})
}
func (s *Store) PrepareOutgoingReaction(ctx context.Context, j delivery.Job, p reactions.OutgoingPlan, c reactions.Settings) (reactions.OutgoingPlan, error) {
	var result reactions.OutgoingPlan
	if p.Key != j.Dedup || p.Session != c.Session || p.State != "pending" || p.NoticeTTL != c.NoticeTTL || !validReactionSettings(c) || !p.Input.Mapped || p.Supported && p.Emoji != "" && !reactions.EmojiEquivalent(p.Emoji) {
		return result, delivery.ErrInvalid
	}
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if owner, old, err := outgoingReactionPlan(ctx, q, row.AccountID, p.Key); err == nil {
			if owner.JobID != row.ID {
				return delivery.ErrInvalid
			}
			result = old
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		actual, err := outgoingReactionInput(ctx, q, row, c)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, p.Input) {
			return delivery.ErrLocalRetry
		}
		if frozen := actual.FrozenChoice; frozen != nil && (p.Emoji != frozen.Emoji || p.Supported != frozen.Supported || p.CustomID != "") {
			return delivery.ErrInvalid
		}
		if !p.Supported {
			p.State = "unsupported"
			notice := control.Notice{Target: p.Input.Target, ReplyTo: p.Input.TelegramMessageID, TTLSeconds: p.NoticeTTL, Body: presentation.Plain("This reaction has no standard emoji equivalent and cannot be forwarded to WhatsApp. Choose a standard emoji reaction instead.")}
			if _, err := metadataEnqueue(ctx, q, row.AccountID, row.Dedup+":reaction-notice", "notice:"+row.Dedup, "control-notice", notice); err != nil {
				return err
			}
		} else {
			if p.CustomID != "" {
				if err := q.SaveCustomReactionEmoji(ctx, db.SaveCustomReactionEmojiParams{AccountID: row.AccountID, CustomID: p.CustomID, Emoji: p.Emoji}); err != nil {
					return err
				}
			}
			raw, err := q.ReactionEcho(ctx, db.ReactionEchoParams{AccountID: row.AccountID, WhatsappChat: p.Input.ChatID, MessageID: p.Input.MessageID})
			if err == nil {
				var echo reactions.Echo
				if json.Unmarshal(raw, &echo) != nil {
					return delivery.ErrInvalid
				}
				equivalent := echo.Emoji == reactions.EmojiKey(p.Emoji)
				if equivalent && p.Input.SelfID != "" {
					raw, err := q.ReactionState(ctx, db.ReactionStateParams{AccountID: row.AccountID, WhatsappChat: p.Input.ChatID, MessageID: p.Input.MessageID})
					if err == nil {
						var state reactions.State
						if json.Unmarshal(raw, &state) != nil {
							return delivery.ErrInvalid
						}
						if actor, known := state.Actors[p.Input.SelfID]; known {
							equivalent = reactions.EmojiKey(actor.Emoji) == reactions.EmojiKey(p.Emoji)
						} else if state.Full || state.Known[p.Input.SelfID] {
							equivalent = p.Emoji == ""
						}
					} else if !errors.Is(err, pgx.ErrNoRows) {
						return err
					}
				}
				if equivalent {
					p.State = "done"
					if err := recordReactionEcho(ctx, q, row, p); err != nil {
						return err
					}
				}
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if err := q.PrepareOutgoingReactionPlan(ctx, db.PrepareOutgoingReactionPlanParams{AccountID: row.AccountID, PlanKey: p.Key, JobID: row.ID, Payload: raw}); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}
func (s *Store) CompleteOutgoingReaction(ctx context.Context, j delivery.Job) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		owner, p, err := outgoingReactionPlan(ctx, q, row.AccountID, row.Dedup)
		if err != nil {
			return err
		}
		if owner.JobID != row.ID || !p.Supported || p.State != "pending" && p.State != "done" {
			return delivery.ErrInvalid
		}
		if p.State == "done" {
			return nil
		}
		p.State = "done"
		if err := recordReactionEcho(ctx, q, row, p); err != nil {
			return err
		}
		return saveOutgoingReaction(ctx, q, row, p)
	})
}

var _ reactions.Store = (*Store)(nil)
