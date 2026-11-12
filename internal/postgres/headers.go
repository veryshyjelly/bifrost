package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func (s *Store) PrepareTopicHeader(ctx context.Context, j delivery.Job, username string, c control.GroupSettings) (control.Plan, error) {
	if !validGroupSettings(c) {
		return control.Plan{}, delivery.ErrInvalid
	}
	var result control.Plan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		var event struct {
			Type string `json:"type"`
			Data struct {
				ChatID       string `json:"chatId"`
				TelegramChat int64  `json:"tgChat"`
				Thread       int64  `json:"thread"`
				ReturnKey    string `json:"returnKey"`
			} `json:"data"`
		}
		if row.Kind != "management" || json.Unmarshal(row.Payload, &event) != nil || event.Type != "new_conversation" || event.Data.ChatID == "" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, known, err := existingGroupPlan(ctx, q, row)
		if err != nil {
			return err
		}
		if known {
			result = p
			return nil
		}
		p = control.Plan{Key: row.Dedup, Target: control.Target{BotID: "master"}, ReplyTTLSeconds: c.ControlTTL, Header: true}
		route, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: event.Data.ChatID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		key, keyErr := q.MirrorReturnKey(ctx, db.MirrorReturnKeyParams{AccountID: row.AccountID, WhatsappChat: event.Data.ChatID})
		if keyErr != nil {
			return keyErr
		}
		current := err == nil && !(route.InboxRetired && route.TelegramChatID == c.Management && route.ThreadID == 0)
		if event.Data.TelegramChat != 0 {
			current = current && route.TelegramChatID == event.Data.TelegramChat && route.ThreadID == event.Data.Thread && key == event.Data.ReturnKey
		}
		if current {
			p.Target = control.Target{BotID: "master", ChatID: route.TelegramChatID, ThreadID: route.ThreadID}
			p.Scope = &control.Context{Known: true, Target: p.Target, Route: conversation.Route{ChatID: route.WhatsappChat, Name: route.Name, TelegramChatID: route.TelegramChatID, ThreadID: route.ThreadID, Mode: conversation.Mode(route.Mode), InboxRetired: route.InboxRetired}}
			group := strings.HasSuffix(route.WhatsappChat, "@g.us")
			var rows [][]models.InlineKeyboardButton
			if group {
				if username == "" {
					return delivery.ErrLocalRetry
				}
				choice, err := createMirrorChoice(ctx, q, row, route, c.OwnerID)
				if err != nil {
					return err
				}
				rows = append(rows, []models.InlineKeyboardButton{{Text: "Create / link Telegram group", URL: "https://t.me/" + username + "?start=mirror_" + choice.ID}})
			}
			var buttons []models.InlineKeyboardButton
			for _, op := range []string{"create", "keep", "mute", "ignore", "read", "history"} {
				if group && op == "create" {
					continue
				}
				labels := map[string]string{"create": "Create bot", "keep": "Keep here", "mute": "Mute", "ignore": "Ignore", "read": "Mark read", "history": "Recent history"}
				a := control.Action{ID: groupToken(), Kind: "operator", Operation: op, AccountID: row.AccountID, BotID: "master", WhatsAppChat: route.WhatsappChat, OwnerID: c.OwnerID, ChatID: route.TelegramChatID, ThreadID: route.ThreadID, ReturnKey: key, ExpiresAt: time.Now().Add(time.Duration(c.ControlTTL) * time.Second).UnixMilli()}
				p.Actions = append(p.Actions, a)
				buttons = append(buttons, models.InlineKeyboardButton{Text: labels[op], CallbackData: op + ":" + a.ID})
			}
			rows = append(rows, buttons[:3], buttons[3:])
			tail := "Choose whether to keep this conversation here or create a dedicated bot."
			if group {
				tail = "Reply to a participant with /create to provision their bot."
			}
			p.Packets = []control.Packet{{Text: route.Name + "\nWhatsApp ID: " + route.WhatsappChat + "\nReply in this topic to send a message. " + tail, Markup: &models.InlineKeyboardMarkup{InlineKeyboard: rows}}}
		}
		if err := saveGroupPlan(ctx, q, row, p); err != nil {
			return err
		}
		for _, a := range p.Actions {
			b, _ := json.Marshal(a)
			if err := q.CreateControlAction(ctx, db.CreateControlActionParams{AccountID: row.AccountID, ActionID: a.ID, PlanKey: row.Dedup, Payload: b}); err != nil {
				return err
			}
		}
		if current {
			scope := "contact"
			if strings.HasSuffix(route.WhatsappChat, "@g.us") {
				scope = "group"
			}
			b, _ := json.Marshal(map[string]any{"id": row.Dedup + ":metadata", "type": "sync_metadata", "data": map[string]string{"scope": scope, "id": route.WhatsappChat, "source": "new", "requestId": row.Dedup}, "at": time.Now().UnixMilli()})
			if _, err := metadataEnqueue(ctx, q, row.AccountID, row.Dedup+":metadata", "metadata-request:"+row.Dedup, "sync-request", json.RawMessage(b)); err != nil {
				return err
			}
		}
		result = p
		return nil
	})
	return result, err
}
func (s *Store) TopicHeaderCurrent(ctx context.Context, j delivery.Job) (bool, error) {
	current := false
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		r, err := q.ControlPlan(ctx, db.ControlPlanParams{AccountID: row.AccountID, PlanKey: row.Dedup})
		if err != nil {
			return err
		}
		p, err := decodeControl(r)
		if err != nil {
			return err
		}
		if !p.Header {
			return delivery.ErrInvalid
		}
		if p.Scope == nil {
			return nil
		}
		route, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: p.Scope.Route.ChatID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		current = route.TelegramChatID == p.Target.ChatID && route.ThreadID == p.Target.ThreadID && route.InboxRetired == p.Scope.Route.InboxRetired
		if len(p.Actions) > 0 {
			key, err := q.MirrorReturnKey(ctx, db.MirrorReturnKeyParams{AccountID: row.AccountID, WhatsappChat: p.Scope.Route.ChatID})
			if err != nil {
				return err
			}
			current = current && key == p.Actions[0].ReturnKey
		}
		return nil
	})
	return current, err
}
func cancelControlNotice(ctx context.Context, q *db.Queries, row db.Job, p control.Plan) error {
	for i := range p.Packets {
		e, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: p.PacketKey(i)})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		switch e.State {
		case "done":
			if err := recordControl(ctx, q, row.AccountID, p, i); err != nil {
				return err
			}
		case "pending":
			if _, err := q.DiscardEffect(ctx, db.DiscardEffectParams{AccountID: row.AccountID, EffectKey: e.EffectKey, JobID: row.ID}); err != nil {
				return err
			}
		case "discarded":
		default:
			return delivery.ErrUncertain
		}
	}
	return q.CompleteControl(ctx, db.CompleteControlParams{AccountID: row.AccountID, PlanKey: row.Dedup})
}
func (s *Store) CancelTopicHeader(ctx context.Context, j delivery.Job) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		r, err := q.ControlPlan(ctx, db.ControlPlanParams{AccountID: row.AccountID, PlanKey: row.Dedup})
		if err != nil {
			return err
		}
		p, err := decodeControl(r)
		if err != nil {
			return err
		}
		if !p.Header {
			return delivery.ErrInvalid
		}
		return cancelControlNotice(ctx, q, row, p)
	})
}

var _ control.GroupStore = (*Store)(nil)
