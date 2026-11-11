package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func operatorContext(ctx context.Context, q *db.Queries, account, bot string, m *models.Message, management int64) (control.Context, error) {
	c := control.Context{Target: control.Target{BotID: bot, ChatID: m.Chat.ID}}
	chat := ""
	if m.Chat.Type == models.ChatTypePrivate {
		if bot == "master" {
			return c, nil
		}
		contact, err := q.ContactForBot(ctx, db.ContactForBotParams{AccountID: account, BotID: bot})
		if errors.Is(err, pgx.ErrNoRows) {
			return c, nil
		}
		if err != nil {
			return c, err
		}
		d, err := directory(ctx, q, account)
		if err != nil {
			return c, err
		}
		chat, err = conversationKey(ctx, q, account, contact, d)
		if err != nil {
			return c, err
		}
	} else {
		r, err := q.OutgoingRoute(ctx, db.OutgoingRouteParams{AccountID: account, TelegramChatID: m.Chat.ID, ThreadID: int64(m.MessageThreadID), ManagementChatID: management})
		if errors.Is(err, pgx.ErrNoRows) {
			if m.IsTopicMessage {
				c.Target.ThreadID = int64(m.MessageThreadID)
			}
			return c, nil
		}
		if err != nil {
			return c, err
		}
		chat = r.WhatsappChat
		c.Target.ThreadID = r.ThreadID
	}
	r, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: account, WhatsappChat: chat})
	if errors.Is(err, pgx.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	c.Known = true
	c.Route = conversation.Route{ChatID: r.WhatsappChat, Name: r.Name, TelegramChatID: r.TelegramChatID, ThreadID: r.ThreadID, Mode: conversation.Mode(r.Mode), InboxRetired: r.InboxRetired}
	return c, nil
}

func (s *Store) OperatorContext(ctx context.Context, job delivery.Job, bot string, m *models.Message, management int64) (control.Context, error) {
	if m == nil || m.Chat.ID == 0 || bot == "" || management >= 0 {
		return control.Context{}, delivery.ErrInvalid
	}
	var result control.Context
	err := s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if row.Kind != "tg" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		var err error
		result, err = operatorContext(ctx, q, row.AccountID, bot, m, management)
		return err
	})
	return result, err
}

func queueOperator(ctx context.Context, q *db.Queries, row db.Job, p control.Plan, management int64) error {
	r := p.Operator
	if r == nil {
		return nil
	}
	if row.Kind != "tg" || !control.IsOperatorCommand(r.Operation) || r.OwnerID <= 0 || management >= 0 || !r.Context.Known || r.Context.Route.ChatID == "status@broadcast" || r.Context.Target != p.Target || p.Command == nil || p.Clear != nil || p.Scope != nil || len(p.Packets) != 0 || len(p.Actions) != 0 || p.Answer != nil {
		return delivery.ErrInvalid
	}
	var source struct {
		Bot    string          `json:"bot"`
		Update json.RawMessage `json:"update"`
	}
	var u models.Update
	if json.Unmarshal(row.Payload, &source) != nil || json.Unmarshal(source.Update, &u) != nil || !ingress.AuthorizedUpdate(source.Update, &u, r.OwnerID) || u.Message == nil || u.EditedMessage != nil || source.Bot != p.Target.BotID || row.Dedup != "tg:"+source.Bot+":"+strconv.FormatInt(u.ID, 10) || int64(u.Message.ID) != p.Command.MessageID || (u.Message.Chat.Type == models.ChatTypePrivate && u.Message.Chat.ID != r.OwnerID) {
		return delivery.ErrInvalid
	}
	parsed, ok := control.ParseCommand(u.Message)
	if !ok || parsed.Name != r.Operation || (parsed.Recipient == "" && u.Message.Chat.Type != models.ChatTypePrivate && source.Bot != "master") {
		return delivery.ErrInvalid
	}
	current, err := operatorContext(ctx, q, row.AccountID, source.Bot, u.Message, management)
	if err != nil {
		return err
	}
	if !current.Known || current.Target != p.Target || current.Route.ChatID != r.Context.Route.ChatID {
		return delivery.ErrLocalRetry
	}
	if reply := u.Message.ReplyToMessage; reply != nil {
		mapping, err := q.TelegramMapping(ctx, db.TelegramMappingParams{AccountID: row.AccountID, Scope: relayScope(source.Bot, p.Target.ChatID), TelegramChatID: p.Target.ChatID, TelegramMessageID: int64(reply.ID)})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil && mapping.WhatsappChat != current.Route.ChatID {
			return delivery.ErrInvalid
		}
	}
	event := control.OperatorEvent{ID: p.Key, Type: "operator_action", At: time.Now().UnixMilli()}
	event.Data.Operation = r.Operation
	event.Data.ChatID = current.Route.ChatID
	event.Data.Sender = current.Route.ChatID
	if r.Operation == "read" {
		event.Type = "mark_read"
	}
	payload, _ := json.Marshal(event)
	_, err = q.Enqueue(ctx, db.EnqueueParams{AccountID: row.AccountID, Dedup: "management:" + p.Key, Stream: "management:" + p.Key, Kind: "management", Payload: payload})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func (s *Store) PrepareOperatorControl(ctx context.Context, job delivery.Job, ttl int) (control.Plan, error) {
	if ttl < 5 || ttl > 86400 {
		return control.Plan{}, delivery.ErrInvalid
	}
	var result control.Plan
	err := s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		var event control.OperatorEvent
		if row.Kind != "management" || json.Unmarshal(row.Payload, &event) != nil || event.Type != "operator_action" || event.Data.ChatID == "" {
			return delivery.ErrInvalid
		}
		if event.Data.Operation != "keep" && event.Data.Operation != "mute" && event.Data.Operation != "ignore" {
			return delivery.ErrUnsupported
		}
		old, err := q.ControlPlan(ctx, db.ControlPlanParams{AccountID: row.AccountID, PlanKey: row.Dedup})
		if err == nil {
			if old.JobID != row.ID {
				return delivery.ErrInvalid
			}
			result, err = decodeControl(old)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		route, err := q.LockControlRoute(ctx, db.LockControlRouteParams{AccountID: row.AccountID, WhatsappChat: event.Data.ChatID})
		if errors.Is(err, pgx.ErrNoRows) {
			return delivery.ErrInvalid
		}
		if err != nil {
			return err
		}
		mode, text := "inbox", "Conversation stays here."
		switch event.Data.Operation {
		case "keep":
			if route.Mode == "group" {
				mode = "group"
			}
		case "mute":
			mode, text = "muted", "Conversation muted. Use /keep to resume."
		case "ignore":
			mode, text = "ignored", "Conversation ignored. Use /keep to resume."
		}
		if err := q.SetConversationMode(ctx, db.SetConversationModeParams{AccountID: row.AccountID, WhatsappChat: route.WhatsappChat, Mode: mode}); err != nil {
			return err
		}
		result = control.Plan{Key: row.Dedup, Target: control.Target{BotID: "master", ChatID: route.TelegramChatID, ThreadID: route.ThreadID}, Packets: []control.Packet{{Text: text}}, ReplyTTLSeconds: ttl}
		payload, _ := json.Marshal(result)
		return q.PrepareControl(ctx, db.PrepareControlParams{AccountID: row.AccountID, PlanKey: row.Dedup, JobID: row.ID, Payload: payload})
	})
	return result, err
}

var _ control.OperatorStore = (*Store)(nil)
