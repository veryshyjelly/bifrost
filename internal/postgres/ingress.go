package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func (s *Store) IngressBot(ctx context.Context, account, id string) (ingress.Bot, bool, error) {
	r, err := s.queries.IngressBot(ctx, db.IngressBotParams{AccountID: account, BotID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return ingress.Bot{}, false, nil
	}
	return ingress.Bot{ID: r.BotID, Username: r.Username}, err == nil, err
}

func (s *Store) PutIngressBot(ctx context.Context, account string, bot ingress.Bot) error {
	if account == "" || bot.ID == "" {
		return delivery.ErrInvalid
	}
	return s.queries.PutIngressBot(ctx, db.PutIngressBotParams{AccountID: account, BotID: bot.ID, Username: bot.Username})
}

func (s *Store) IngressRoute(ctx context.Context, account string, chat, thread, management int64) (ingress.Route, bool, error) {
	r, err := s.queries.IngressRoute(ctx, db.IngressRouteParams{AccountID: account, TelegramChatID: chat, ThreadID: thread, ManagementChatID: management})
	if errors.Is(err, pgx.ErrNoRows) {
		return ingress.Route{}, false, nil
	}
	return ingress.Route{WhatsAppChat: r.WhatsappChat, TelegramChatID: r.TelegramChatID, ThreadID: r.ThreadID, InboxRetired: r.InboxRetired}, err == nil, err
}

func (s *Store) PutIngressRoute(ctx context.Context, account string, route ingress.Route) error {
	if account == "" || route.WhatsAppChat == "" || route.TelegramChatID == 0 || route.ThreadID < 0 {
		return delivery.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.queries.WithTx(tx)
	if err := q.LockContactDirectory(ctx, account); err != nil {
		return err
	}
	busy, err := q.RouteTransitionBusy(ctx, db.RouteTransitionBusyParams{AccountID: account, WhatsappChat: route.WhatsAppChat, JobID: 0})
	if err != nil {
		return err
	}
	if busy {
		return delivery.ErrLocalRetry
	}
	reserved, err := q.ReservedTopic(ctx, db.ReservedTopicParams{AccountID: account, TelegramChatID: route.TelegramChatID, ThreadID: route.ThreadID})
	if err != nil {
		return err
	}
	if reserved {
		return conversation.ErrInboxRequired
	}
	status, err := q.StatusTopicInUse(ctx, db.StatusTopicInUseParams{AccountID: account, Management: route.TelegramChatID, ThreadID: route.ThreadID})
	if err != nil {
		return err
	}
	if status {
		return delivery.ErrInvalid
	}
	if err := q.PutIngressRoute(ctx, db.PutIngressRouteParams{AccountID: account, WhatsappChat: route.WhatsAppChat, TelegramChatID: route.TelegramChatID, ThreadID: route.ThreadID, InboxRetired: route.InboxRetired}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Admit(ctx context.Context, event *delivery.Event, observation *ingress.Observation) (bool, error) {
	if event == nil && observation == nil {
		return false, nil
	}
	if event != nil && (event.AccountID == "" || event.Dedup == "" || event.Stream == "" || event.Kind == "" || !json.Valid(event.Payload)) {
		return false, delivery.ErrInvalid
	}
	if observation != nil && (observation.AccountID == "" || observation.Scope == "" || observation.Bot == "" || observation.ChatID == 0 || observation.MessageID <= 0 || !json.Valid(observation.Payload)) {
		return false, delivery.ErrInvalid
	}
	if event != nil && observation != nil && event.AccountID != observation.AccountID {
		return false, delivery.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	q := s.queries.WithTx(tx)
	if o := observation; o != nil {
		if err := q.ObserveTelegram(ctx, db.ObserveTelegramParams{AccountID: o.AccountID, Scope: o.Scope, BotID: o.Bot, ChatID: o.ChatID, MessageID: o.MessageID, ThreadID: o.ThreadID, Kind: o.Kind, MessageDate: o.Date, Payload: o.Payload}); err != nil {
			return false, err
		}
	}
	accepted := false
	if e := event; e != nil {
		if e.Kind == "wa" {
			accepted, err = s.admitWhatsApp(ctx, tx, *e)
			if err != nil {
				return false, err
			}
		} else {
			_, err := q.Enqueue(ctx, db.EnqueueParams{AccountID: e.AccountID, Dedup: e.Dedup, Stream: e.Stream, Kind: e.Kind, Payload: e.Payload})
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return false, err
			}
			accepted = err == nil
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return accepted, nil
}
