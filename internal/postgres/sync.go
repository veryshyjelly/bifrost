package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func validSyncSettings(c control.SyncSettings) bool {
	return c.Session != "" && c.OwnerID > 0 && c.Management < 0 && c.CommandTTL >= 5 && c.CommandTTL <= 86400 && c.ControlTTL >= 5 && c.ControlTTL <= 86400
}

func validSyncPermission(ctx context.Context, q *db.Queries, row db.Job, id string, chat int64, c control.SyncSettings) (control.SyncPermission, bool, error) {
	raw, err := q.MetadataPermission(ctx, db.MetadataPermissionParams{AccountID: row.AccountID, ActionID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return control.SyncPermission{}, false, nil
	}
	if err != nil {
		return control.SyncPermission{}, false, err
	}
	var p control.SyncPermission
	if json.Unmarshal(raw, &p) != nil {
		return p, false, delivery.ErrInvalid
	}
	if p.ID != id || p.AccountID != row.AccountID || p.OwnerID != c.OwnerID || p.Session != c.Session || p.ExpiresAt < time.Now().UnixMilli() || p.ChatID != chat || chat >= 0 || chat == c.Management {
		return p, false, nil
	}
	route, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat})
	if errors.Is(err, pgx.ErrNoRows) {
		return p, false, nil
	}
	if err != nil {
		return p, false, err
	}
	key, err := q.MirrorReturnKey(ctx, db.MirrorReturnKeyParams{AccountID: row.AccountID, WhatsappChat: p.WhatsAppChat})
	return p, strings.HasSuffix(p.WhatsAppChat, "@g.us") && route.TelegramChatID == chat && route.ThreadID == 0 && key == p.ReturnKey, err
}

func (s *Store) PrepareSyncControl(ctx context.Context, j delivery.Job, c control.SyncSettings) (control.Plan, error) {
	if !validSyncSettings(c) {
		return control.Plan{}, delivery.ErrInvalid
	}
	var result control.Plan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		var source struct {
			Bot    string          `json:"bot"`
			Update json.RawMessage `json:"update"`
		}
		var u models.Update
		if row.Kind != "tg" || json.Unmarshal(row.Payload, &source) != nil || json.Unmarshal(source.Update, &u) != nil || !ingress.AuthorizedUpdate(source.Update, &u, c.OwnerID) || row.Dedup != "tg:"+source.Bot+":"+strconv.FormatInt(u.ID, 10) || u.Message == nil || u.EditedMessage != nil {
			return delivery.ErrInvalid
		}
		m := u.Message
		if m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != c.OwnerID {
			return delivery.ErrInvalid
		}
		parsed, ok := control.ParseCommand(m)
		if !ok {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if p, known, err := existingGroupPlan(ctx, q, row); err != nil || known {
			result = p
			return err
		}
		context, err := operatorContext(ctx, q, row.AccountID, source.Bot, m, c.Management)
		if err != nil {
			return err
		}
		p := control.Plan{Key: row.Dedup, Target: context.Target, ReplyTTLSeconds: c.CommandTTL, Command: &control.Command{MessageID: int64(m.ID), Date: int64(m.Date) * 1000, TTLSeconds: c.CommandTTL}}
		scope, id, text := "", "", ""
		directReply := false
		switch {
		case parsed.Name == "start" && source.Bot == "master" && m.Chat.Type != models.ChatTypePrivate && len(parsed.Args) > 0 && strings.HasPrefix(parsed.Args[0], "sync_"):
			permission, valid, err := validSyncPermission(ctx, q, row, strings.TrimPrefix(parsed.Args[0], "sync_"), m.Chat.ID, c)
			if err != nil {
				return err
			}
			if !valid {
				text = control.SyncPermissionExpired
			} else {
				scope, id, text = "group", permission.WhatsAppChat, control.SyncPermissionQueued
			}
		case parsed.Name != "sync":
			return delivery.ErrUnsupported
		case source.Bot == "master" && (len(parsed.Args) > 0 && parsed.Args[0] == "all" || len(parsed.Args) == 0 && (m.Chat.Type == models.ChatTypePrivate || m.Chat.ID == c.Management && context.Target.ThreadID == 0)):
			if m.Chat.Type != models.ChatTypePrivate && m.Chat.ID != c.Management {
				text = "Use /sync all in your private chat with Bifrost or in the management group."
			} else {
				scope, text = "all", "Metadata sync queued. WhatsApp group and contact details will be checked in the background."
			}
		case source.Bot != "master":
			if len(parsed.Args) > 0 {
				text = "Use /sync here for this contact, or /sync all in your private chat with the master bot."
				directReply = true
			} else {
				bound, err := q.ContactForBot(ctx, db.ContactForBotParams{AccountID: row.AccountID, BotID: source.Bot})
				if errors.Is(err, pgx.ErrNoRows) {
					text = "Connect this bot to a WhatsApp contact in the master bot first: select the contact in its binding message, then run /sync here again."
				} else if err != nil {
					return err
				} else {
					scope, id, text = "contact", bound, "Metadata sync queued for this contact."
					directReply = true
				}
			}
		default:
			if context.Known && m.ReplyToMessage != nil {
				mapping, err := q.TelegramMapping(ctx, db.TelegramMappingParams{AccountID: row.AccountID, Scope: relayScope(source.Bot, p.Target.ChatID), TelegramChatID: p.Target.ChatID, TelegramMessageID: int64(m.ReplyToMessage.ID)})
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
				if err == nil && mapping.WhatsappChat != context.Route.ChatID {
					return delivery.ErrInvalid
				}
			}
			if !context.Known {
				text = "Use this command inside a linked conversation or its management topic."
			} else if len(parsed.Args) > 0 {
				text = "Use /sync here for this conversation, or /sync all in your private chat with Bifrost."
			} else {
				scope, id, text = "contact", context.Route.ChatID, "Metadata sync queued for this conversation."
				if strings.HasSuffix(id, "@g.us") {
					scope = "group"
				}
			}
		}
		if scope != "" {
			payload, _ := json.Marshal(map[string]any{"id": row.Dedup, "type": "sync_metadata", "data": map[string]any{"scope": scope, "id": id, "notify": map[string]any{"bot": source.Bot, "chat": context.Target.ChatID, "thread": context.Target.ThreadID}}, "at": time.Now().UnixMilli()})
			if err := enqueueTransitionEvent(ctx, q, row.AccountID, "management:"+row.Dedup, payload); err != nil {
				return err
			}
		}
		packet := control.Packet{Text: text}
		if directReply && p.Target.ThreadID != 0 {
			packet.Target = &control.Target{BotID: source.Bot, ChatID: m.Chat.ID}
		}
		p.Packets = []control.Packet{packet}
		if err := saveGroupPlan(ctx, q, row, p); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}

var _ control.SyncStore = (*Store)(nil)
