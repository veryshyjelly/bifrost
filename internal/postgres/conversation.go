package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func (s *Store) ConversationRoute(ctx context.Context, account, chat string) (conversation.Route, bool, error) {
	r, err := s.queries.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: account, WhatsappChat: chat})
	if errors.Is(err, pgx.ErrNoRows) {
		return conversation.Route{}, false, nil
	}
	return conversation.Route{ChatID: r.WhatsappChat, Name: r.Name, TelegramChatID: r.TelegramChatID, ThreadID: r.ThreadID, Mode: conversation.Mode(r.Mode), InboxRetired: r.InboxRetired}, err == nil, err
}
func (s *Store) ContactBot(ctx context.Context, account, contact string) (conversation.Binding, bool, error) {
	d, err := s.IdentityDirectory(ctx, account)
	if err != nil {
		return conversation.Binding{}, false, err
	}
	p := d.Person(contact, nil)
	return conversation.Binding{BotID: p.Bot.ID, Started: p.Bot.Started}, p.Registered, nil
}

// CreateInbox is fenced against stale workers and never overwrites an existing
// binding. Its management notification and new route are one transaction.
func (s *Store) CreateInbox(ctx context.Context, job delivery.Job, route conversation.Route) (conversation.Route, error) {
	if (!route.InboxRetired && route.ThreadID <= 0) || (route.InboxRetired && route.Mode != conversation.Dedicated) || route.ChatID == "" || route.TelegramChatID >= 0 || route.ThreadID < 0 || (route.Mode != conversation.Inbox && route.Mode != conversation.Dedicated) {
		return conversation.Route{}, delivery.ErrInvalid
	}
	var result conversation.Route
	err := s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if row.Kind != "wa" || row.Stream != "wa:"+route.ChatID {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		reserved, err := q.ReservedTopic(ctx, db.ReservedTopicParams{AccountID: row.AccountID, TelegramChatID: route.TelegramChatID, ThreadID: route.ThreadID})
		if err != nil {
			return err
		}
		if reserved {
			return conversation.ErrInboxRequired
		}
		r, err := q.CreateInbox(ctx, db.CreateInboxParams{AccountID: row.AccountID, WhatsappChat: route.ChatID, Name: route.Name, TelegramChatID: route.TelegramChatID, ThreadID: route.ThreadID, Mode: string(route.Mode), InboxRetired: route.InboxRetired})
		if errors.Is(err, pgx.ErrNoRows) {
			r, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: route.ChatID})
			if err != nil {
				return err
			}
			result = conversation.Route{ChatID: r.WhatsappChat, Name: r.Name, TelegramChatID: r.TelegramChatID, ThreadID: r.ThreadID, Mode: conversation.Mode(r.Mode), InboxRetired: r.InboxRetired}
			return nil
		}
		if err != nil {
			return err
		}
		result = conversation.Route{ChatID: r.WhatsappChat, Name: r.Name, TelegramChatID: r.TelegramChatID, ThreadID: r.ThreadID, Mode: conversation.Mode(r.Mode), InboxRetired: r.InboxRetired}
		if route.InboxRetired {
			return nil
		}
		if route.ThreadID > 1 {
			if err := q.RememberInbox(ctx, db.RememberInboxParams{AccountID: row.AccountID, WhatsappChat: route.ChatID, TelegramChatID: route.TelegramChatID, ThreadID: route.ThreadID}); err != nil {
				return err
			}
		}
		key := "new:" + route.ChatID
		payload, _ := json.Marshal(map[string]any{"id": key, "type": "new_conversation", "data": map[string]any{"chatId": route.ChatID, "tgChat": route.TelegramChatID, "thread": route.ThreadID, "returnKey": ""}, "at": time.Now().UnixMilli()})
		_, err = q.Enqueue(ctx, db.EnqueueParams{AccountID: row.AccountID, Dedup: "management:" + key, Stream: "management:" + key, Kind: "management", Payload: payload})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	return result, err
}

// BindContactBot takes a canonical, verified contact identity. Alias verification
// belongs to the identity workflow; an existing bot cannot bind to two people.
func (s *Store) BindContactBot(ctx context.Context, job delivery.Job, contact, botID string) error {
	id, err := strconv.ParseInt(botID, 10, 64)
	if contact == "" || err != nil || id <= 0 {
		return delivery.ErrInvalid
	}
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if row.Kind != "tg" && row.Kind != "management" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		contact = d.Canonical(contact)
		err = q.BindContactBot(ctx, db.BindContactBotParams{AccountID: row.AccountID, ContactID: contact, BotID: botID})
		var failure *pgconn.PgError
		if errors.As(err, &failure) && (failure.Code == "23505" || failure.Code == "23503") {
			return delivery.ErrInvalid
		}
		if err != nil {
			return err
		}
		if _, err := directory(ctx, q, row.AccountID); err != nil {
			return err
		}
		chat, err := conversationKey(ctx, q, row.AccountID, contact, d)
		if err != nil {
			return err
		}
		return q.BindDirectRouteMode(ctx, db.BindDirectRouteModeParams{AccountID: row.AccountID, WhatsappChat: chat})
	})
}

func (s *Store) StartContactBot(ctx context.Context, job delivery.Job, botID string) error {
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if row.Kind != "tg" && row.Kind != "management" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		contact, err := q.ContactForBot(ctx, db.ContactForBotParams{AccountID: row.AccountID, BotID: botID})
		if errors.Is(err, pgx.ErrNoRows) {
			return delivery.ErrInvalid
		}
		if err != nil {
			return err
		}
		n, err := q.SetBotStarted(ctx, db.SetBotStartedParams{AccountID: row.AccountID, BotID: botID})
		if err != nil {
			return err
		}
		if n != 1 {
			return delivery.ErrInvalid
		}
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		chat, err := conversationKey(ctx, q, row.AccountID, contact, d)
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"chatId": chat})
		_, err = q.Enqueue(ctx, db.EnqueueParams{AccountID: row.AccountID, Dedup: "retire-inbox:" + row.Dedup, Stream: "retire-inbox:" + chat, Kind: "control-retire-inbox", Payload: payload})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
}
