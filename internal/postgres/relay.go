package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

func relayScope(bot string, chat int64) string {
	if chat < 0 {
		return "*"
	}
	return bot
}
func loadRelay(ctx context.Context, q *db.Queries, account, key string) (relay.Plan, error) {
	r, err := q.RelayPlan(ctx, db.RelayPlanParams{AccountID: account, PlanKey: key})
	if err != nil {
		return relay.Plan{}, err
	}
	var p relay.Plan
	if json.Unmarshal(r.Payload, &p) != nil || p.Key != key {
		return relay.Plan{}, delivery.ErrInvalid
	}
	p.Complete = r.Complete
	return p, nil
}
func (s *Store) RelayPlan(ctx context.Context, account, key string) (relay.Plan, bool, error) {
	p, err := loadRelay(ctx, s.queries, account, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return relay.Plan{}, false, nil
	}
	return p, err == nil, err
}
func (s *Store) PrepareRelay(ctx context.Context, job delivery.Job, p relay.Plan) (relay.Plan, error) {
	if p.Key == "" || p.Prefix == "" || p.WhatsAppChat == "" || p.TelegramChat == 0 || p.BotID == "" || (p.Direction != "in" && p.Direction != "out") || (p.Direction == "in" && len(p.Parts) == 0) || (p.Direction == "out" && (p.Outgoing == nil || p.TelegramMessage <= 0)) {
		return relay.Plan{}, delivery.ErrInvalid
	}
	var result relay.Plan
	err := s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		existing, err := loadRelay(ctx, q, row.AccountID, p.Key)
		if err == nil {
			result = existing
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if (p.Direction == "in" && (row.Kind != "wa" || row.Stream != "wa:"+p.WhatsAppChat)) || (p.Direction == "out" && row.Kind != "tg") {
			return delivery.ErrInvalid
		}
		route, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat})
		if err != nil {
			return err
		}
		if route.TelegramChatID != p.Route.TelegramChatID || route.ThreadID != p.Route.ThreadID || route.Mode != string(p.Route.Mode) || route.InboxRetired != p.Route.InboxRetired || route.Mode == "muted" || route.Mode == "ignored" {
			return delivery.ErrLocalRetry
		}
		payload, err := json.Marshal(p)
		if err != nil {
			return delivery.ErrInvalid
		}
		if err := q.PrepareRelay(ctx, db.PrepareRelayParams{AccountID: row.AccountID, PlanKey: p.Key, JobID: row.ID, Direction: p.Direction, WhatsappChat: p.WhatsAppChat, Payload: payload}); err != nil {
			return err
		}
		result, err = loadRelay(ctx, q, row.AccountID, p.Key)
		return err
	})
	return result, err
}
func (s *Store) RelayEcho(ctx context.Context, account, chat, id string) (relay.Echo, error) {
	r, err := s.queries.RelayEcho(ctx, db.RelayEchoParams{AccountID: account, WhatsappChat: chat, WhatsappMessageID: id})
	return relay.Echo{Recognized: r.Recognized.Bool, Pending: r.Pending}, err
}
func (s *Store) WhatsAppQuote(ctx context.Context, account, chat, id, bot string, tgChat, thread int64) (relay.Mapping, bool, error) {
	r, err := s.queries.WhatsAppQuote(ctx, db.WhatsAppQuoteParams{AccountID: account, WhatsappChat: chat, WhatsappMessageID: id, Scope: relayScope(bot, tgChat), TelegramChatID: tgChat, ThreadID: thread})
	if errors.Is(err, pgx.ErrNoRows) {
		return relay.Mapping{}, false, nil
	}
	return relay.Mapping{WhatsAppChat: r.WhatsappChat, WhatsAppMessage: r.WhatsappMessageID, BotID: r.BotID, TelegramChat: r.TelegramChatID, TelegramMessage: r.TelegramMessageID, ThreadID: r.ThreadID, Direction: r.Direction, Deleted: r.Deleted}, err == nil, err
}
func (s *Store) TelegramMapping(ctx context.Context, account, bot string, chat, id int64) (relay.Mapping, bool, error) {
	r, err := s.queries.TelegramMapping(ctx, db.TelegramMappingParams{AccountID: account, Scope: relayScope(bot, chat), TelegramChatID: chat, TelegramMessageID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return relay.Mapping{}, false, nil
	}
	return relay.Mapping{WhatsAppChat: r.WhatsappChat, WhatsAppMessage: r.WhatsappMessageID, BotID: r.BotID, TelegramChat: r.TelegramChatID, TelegramMessage: r.TelegramMessageID, ThreadID: r.ThreadID, Direction: r.Direction, Deleted: r.Deleted || r.ObservationDeleted}, err == nil, err
}

func (s *Store) OutgoingRoute(ctx context.Context, job delivery.Job, bot string, chat, thread, management int64) (conversation.Route, bool, error) {
	if chat > 0 {
		if bot == "master" {
			return conversation.Route{}, false, nil
		}
		var route conversation.Route
		err := s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
			if row.Kind != "tg" {
				return delivery.ErrInvalid
			}
			if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
				return err
			}
			contact, err := q.ContactForBot(ctx, db.ContactForBotParams{AccountID: row.AccountID, BotID: bot})
			if errors.Is(err, pgx.ErrNoRows) {
				return delivery.ErrUnsupported
			}
			if err != nil {
				return err
			}
			binding, err := q.ContactBot(ctx, db.ContactBotParams{AccountID: row.AccountID, ContactID: contact})
			if err != nil {
				return err
			}
			if !binding.Started {
				return delivery.ErrUnsupported
			}
			d, err := directory(ctx, q, row.AccountID)
			if err != nil {
				return err
			}
			contact, err = conversationKey(ctx, q, row.AccountID, contact, d)
			if err != nil {
				return err
			}
			r, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: contact})
			if errors.Is(err, pgx.ErrNoRows) {
				_, err = q.CreateInbox(ctx, db.CreateInboxParams{AccountID: row.AccountID, WhatsappChat: contact, Name: contact, TelegramChatID: management, Mode: "dedicated", InboxRetired: true})
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
				r, err = q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: contact})
			}
			if err != nil {
				return err
			}
			route = conversation.Route{ChatID: r.WhatsappChat, Name: r.Name, TelegramChatID: r.TelegramChatID, ThreadID: r.ThreadID, Mode: conversation.Mode(r.Mode), InboxRetired: r.InboxRetired}
			return nil
		})
		return route, err == nil, err
	}
	r, err := s.queries.OutgoingRoute(ctx, db.OutgoingRouteParams{AccountID: job.AccountID, TelegramChatID: chat, ThreadID: thread, ManagementChatID: management})
	if errors.Is(err, pgx.ErrNoRows) {
		return conversation.Route{}, false, nil
	}
	return conversation.Route{ChatID: r.WhatsappChat, Name: r.Name, TelegramChatID: r.TelegramChatID, ThreadID: r.ThreadID, Mode: conversation.Mode(r.Mode), InboxRetired: r.InboxRetired}, err == nil, err
}

// recordRelay reconciles from the ledger's immutable receipt, not caller JSON.
func recordRelay(ctx context.Context, q *db.Queries, account string, p relay.Plan, part int) (bool, error) {
	key, _, err := p.FrozenRequest(part)
	if err != nil {
		return false, err
	}
	effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: account, EffectKey: key})
	if err != nil {
		return false, err
	}
	if effect.State == "discarded" {
		return false, nil
	}
	if effect.State != "done" {
		return false, delivery.ErrUncertain
	}
	waID := p.WhatsAppMessage
	tgID := p.TelegramMessage
	date := time.Now().UnixMilli()
	if p.Direction == "in" {
		var message models.Message
		if json.Unmarshal(effect.Result, &message) != nil || message.ID <= 0 || message.Chat.ID != p.TelegramChat {
			return false, delivery.ErrUncertain
		}
		tgID = int64(message.ID)
		if message.Date > 0 {
			date = int64(message.Date) * 1000
		}
	} else {
		var receipt sdk.MessageResponse
		if json.Unmarshal(effect.Result, &receipt) != nil || receipt.MessageID == "" {
			return false, delivery.ErrUncertain
		}
		waID = receipt.MessageID
	}
	scope := relayScope(p.BotID, p.TelegramChat)
	if err := q.InsertRelayMapping(ctx, db.InsertRelayMappingParams{AccountID: account, Scope: scope, TelegramChatID: p.TelegramChat, TelegramMessageID: tgID, ThreadID: p.ThreadID, BotID: p.BotID, WhatsappChat: p.WhatsAppChat, WhatsappMessageID: waID, Direction: p.Direction, PlanKey: pgtype.Text{String: p.Key, Valid: true}, Part: int32(part), Receipt: effect.Result}); err != nil {
		return false, err
	}
	check, err := q.TelegramMapping(ctx, db.TelegramMappingParams{AccountID: account, Scope: scope, TelegramChatID: p.TelegramChat, TelegramMessageID: tgID})
	if err != nil {
		return false, err
	}
	if check.PlanKey.String != p.Key || check.Part != int32(part) || check.WhatsappChat != p.WhatsAppChat || check.WhatsappMessageID != waID || check.ThreadID != p.ThreadID {
		return false, delivery.ErrInvalid
	}
	payload := effect.Result
	if p.Direction == "out" {
		payload, _ = json.Marshal(map[string]any{"message_id": tgID, "chat": map[string]int64{"id": p.TelegramChat}, "message_thread_id": p.ThreadID})
	}
	if err := q.ObserveTelegram(ctx, db.ObserveTelegramParams{AccountID: account, Scope: scope, BotID: p.BotID, ChatID: p.TelegramChat, MessageID: tgID, ThreadID: p.ThreadID, Kind: "chat", MessageDate: date, Payload: payload}); err != nil {
		return false, err
	}
	return true, nil
}
func (s *Store) RecordRelay(ctx context.Context, job delivery.Job, key string, part int) error {
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		p, err := loadRelay(ctx, q, row.AccountID, key)
		if err != nil {
			return err
		}
		_, err = recordRelay(ctx, q, row.AccountID, p, part)
		return err
	})
}

func finishRelay(ctx context.Context, q *db.Queries, account string, p relay.Plan) error {
	n := len(p.Parts)
	if p.Direction == "out" {
		n = 1
	}
	sent := false
	for part := 0; part < n; part++ {
		done, err := recordRelay(ctx, q, account, p, part)
		if err != nil {
			return err
		}
		sent = sent || done
	}
	if p.Direction == "out" && sent {
		key := p.Prefix + ":read"
		payload, _ := json.Marshal(map[string]any{"id": key, "type": "mark_read", "data": map[string]string{"chatId": p.WhatsAppChat}, "at": time.Now().UnixMilli()})
		_, err := q.Enqueue(ctx, db.EnqueueParams{AccountID: account, Dedup: "management:" + key, Stream: "management:" + key, Kind: "management", Payload: payload})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	return q.CompleteRelay(ctx, db.CompleteRelayParams{AccountID: account, PlanKey: p.Key})
}
func (s *Store) CompleteRelay(ctx context.Context, job delivery.Job, key string) error {
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		p, err := loadRelay(ctx, q, row.AccountID, key)
		if err != nil {
			return err
		}
		return finishRelay(ctx, q, row.AccountID, p)
	})
}

// CancelRelay applies an explicit muted/ignored route policy to definitely unsent
// parts only. It cannot discard a sending/uncertain effect or erase a receipt.
func (s *Store) CancelRelay(ctx context.Context, job delivery.Job, key string) error {
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		p, err := loadRelay(ctx, q, row.AccountID, key)
		if err != nil {
			return err
		}
		route, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat})
		if err != nil {
			return err
		}
		if route.Mode != "muted" && route.Mode != "ignored" {
			return delivery.ErrInvalid
		}
		n := len(p.Parts)
		if p.Direction == "out" {
			n = 1
		}
		for part := 0; part < n; part++ {
			effectKey, request, err := p.FrozenRequest(part)
			if err != nil {
				return err
			}
			if err := q.CreateEffect(ctx, db.CreateEffectParams{AccountID: row.AccountID, EffectKey: effectKey, JobID: row.ID, Request: request}); err != nil {
				return err
			}
			effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: effectKey})
			if err != nil {
				return err
			}
			switch effect.State {
			case "done", "discarded":
				continue
			case "pending":
				if effect.JobID != row.ID {
					return delivery.ErrInvalid
				}
				if n, err := q.DiscardEffect(ctx, db.DiscardEffectParams{AccountID: row.AccountID, EffectKey: effectKey, JobID: row.ID}); err != nil {
					return err
				} else if n != 1 {
					return delivery.ErrInvalid
				}
			default:
				return delivery.ErrUncertain
			}
		}
		return finishRelay(ctx, q, row.AccountID, p)
	})
}

var _ relay.Store = (*Store)(nil)
