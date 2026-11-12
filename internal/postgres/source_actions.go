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
	"github.com/veryshyjelly/bifrost/internal/sourceactions"
)

func validSourceSettings(c sourceactions.Settings) bool {
	return c.Session != "" && c.OwnerID > 0 && c.Management < 0 && c.CommandTTL >= 5 && c.CommandTTL <= 86400 && c.NoticeTTL >= 5 && c.NoticeTTL <= 86400
}
func admitSourceAction(ctx context.Context, q *db.Queries, row db.Job, action sourceactions.Action) error {
	if _, err := q.SourceActionOrigin(ctx, db.SourceActionOriginParams{AccountID: row.AccountID, SourceJobID: row.ID}); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	action.Key = row.Dedup + ":source-action"
	action.State = "pending"
	job, err := metadataEnqueue(ctx, q, row.AccountID, action.Key, "source-action:"+action.ChatID+":"+action.MessageID, "control-source-action", action)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(action)
	if err != nil {
		return err
	}
	return q.CreateSourceAction(ctx, db.CreateSourceActionParams{AccountID: row.AccountID, JobID: job, SourceJobID: row.ID, Payload: raw})
}
func (s *Store) PrepareSourceCommand(ctx context.Context, j delivery.Job, c sourceactions.Settings) (control.Plan, error) {
	var result control.Plan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if !validSourceSettings(c) || row.Kind != "tg" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if old, err := q.ControlPlan(ctx, db.ControlPlanParams{AccountID: row.AccountID, PlanKey: row.Dedup}); err == nil {
			if old.JobID != row.ID {
				return delivery.ErrInvalid
			}
			result, err = decodeControl(old)
			return err
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var source struct {
			Bot    string          `json:"bot"`
			Update json.RawMessage `json:"update"`
		}
		var u models.Update
		if json.Unmarshal(row.Payload, &source) != nil || source.Bot == "" || json.Unmarshal(source.Update, &u) != nil || u.Message == nil || u.EditedMessage != nil || row.Dedup != "tg:"+source.Bot+":"+strconv.FormatInt(u.ID, 10) || !ingress.AuthorizedUpdate(source.Update, &u, c.OwnerID) {
			return delivery.ErrInvalid
		}
		m := u.Message
		parsed, ok := control.ParseCommand(m)
		if !ok || parsed.Name != "delete" && parsed.Name != "react" || m.From == nil || m.From.IsBot || m.From.ID != c.OwnerID || m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != c.OwnerID || parsed.Recipient == "" && m.Chat.Type != models.ChatTypePrivate && source.Bot != "master" {
			return delivery.ErrInvalid
		}
		current, err := operatorContext(ctx, q, row.AccountID, source.Bot, m, c.Management)
		if err != nil {
			return err
		}
		p := control.Plan{Key: row.Dedup, Target: current.Target, ReplyTTLSeconds: c.NoticeTTL, Command: &control.Command{MessageID: int64(m.ID), Date: int64(m.Date) * 1000, TTLSeconds: c.CommandTTL}}
		if !current.Known {
			p.Packets = []control.Packet{{Text: "Use this command inside a linked conversation or its management topic."}}
		} else if current.Route.ChatID == "status@broadcast" {
			p.Packets = []control.Packet{{Text: "This topic archives WhatsApp Status posts. Open the person's chat to act on a conversation message."}}
		} else if m.ReplyToMessage == nil {
			p.Packets = []control.Packet{{Text: "Reply to the message you want to act on."}}
		} else {
			mapping, err := q.TelegramMapping(ctx, db.TelegramMappingParams{AccountID: row.AccountID, Scope: relayScope(source.Bot, m.Chat.ID), TelegramChatID: m.Chat.ID, TelegramMessageID: int64(m.ReplyToMessage.ID)})
			if errors.Is(err, pgx.ErrNoRows) {
				p.Packets = []control.Packet{{Text: "Reply to the message you want to act on."}}
			} else if err != nil {
				return err
			} else if mapping.WhatsappChat != current.Route.ChatID || mapping.ThreadID != current.Target.ThreadID {
				return delivery.ErrInvalid
			} else {
				emoji := ""
				if len(parsed.Args) > 0 {
					emoji = parsed.Args[0]
				}
				action := sourceactions.Action{Session: c.Session, Operation: parsed.Name, ChatID: current.Route.ChatID, MessageID: mapping.WhatsappMessageID, Emoji: emoji, Target: current.Target, NoticeTTL: c.NoticeTTL}
				if err := admitSourceAction(ctx, q, row, action); err != nil {
					return err
				}
			}
		}
		// Command expiry and source action admission commit together, even when the
		// valid command intentionally produces no Telegram confirmation packet.
		if err := observeSourceCommand(ctx, q, row, p); err != nil {
			return err
		}
		raw, _ := json.Marshal(p)
		if err := q.PrepareControl(ctx, db.PrepareControlParams{AccountID: row.AccountID, PlanKey: p.Key, JobID: row.ID, Payload: raw}); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}
func observeSourceCommand(ctx context.Context, q *db.Queries, row db.Job, p control.Plan) error {
	if p.Command == nil {
		return delivery.ErrInvalid
	}
	command := p.Command
	if err := q.ObserveControl(ctx, db.ObserveControlParams{AccountID: row.AccountID, Scope: relayScope(p.Target.BotID, p.Target.ChatID), BotID: p.Target.BotID, ChatID: p.Target.ChatID, MessageID: command.MessageID, ThreadID: p.Target.ThreadID, Kind: "command", MessageDate: command.Date, Payload: row.Payload, ExpiresAt: time.Now().Add(time.Duration(command.TTLSeconds) * time.Second).UnixMilli()}); err != nil {
		return err
	}
	return q.ScheduleControlExpiry(ctx, db.ScheduleControlExpiryParams{AccountID: row.AccountID, Scope: relayScope(p.Target.BotID, p.Target.ChatID), ChatID: p.Target.ChatID, MessageID: command.MessageID})
}
func (s *Store) AdmitSourceManagement(ctx context.Context, j delivery.Job, c sourceactions.Settings) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "management" || !validSourceSettings(c) {
			return delivery.ErrInvalid
		}
		var source struct {
			Type string `json:"type"`
			Data struct {
				Operation string `json:"operation"`
				ChatID    string `json:"chatId"`
				Message   string `json:"message"`
				Emoji     string `json:"emoji"`
			} `json:"data"`
		}
		if json.Unmarshal(row.Payload, &source) != nil || source.Type != "operator_action" || (source.Data.Operation != "delete" && source.Data.Operation != "react") || source.Data.ChatID == "" || source.Data.Message == "" || source.Data.ChatID == "status@broadcast" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		// Management ingress is owner-authenticated and explicitly names the source
		// message. It retains the legacy direct action surface without requiring a
		// Telegram reply in this separate transport.
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		chat, err := conversationKey(ctx, q, row.AccountID, source.Data.ChatID, d)
		if err != nil {
			return err
		}
		action := sourceactions.Action{Session: c.Session, Operation: source.Data.Operation, ChatID: chat, MessageID: source.Data.Message, Emoji: source.Data.Emoji, NoticeTTL: c.NoticeTTL, Target: control.Target{BotID: "master", ChatID: c.Management}}
		return admitSourceAction(ctx, q, row, action)
	})
}
func (s *Store) SourceAction(ctx context.Context, j delivery.Job) (sourceactions.Action, error) {
	var result sourceactions.Action
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "control-source-action" {
			return delivery.ErrInvalid
		}
		stored, err := q.SourceActionJob(ctx, db.SourceActionJobParams{AccountID: row.AccountID, JobID: row.ID})
		if err != nil {
			return err
		}
		var action, payload sourceactions.Action
		if json.Unmarshal(stored.Payload, &action) != nil || json.Unmarshal(row.Payload, &payload) != nil {
			return delivery.ErrInvalid
		}
		frozen := action
		frozen.State = "pending"
		if !reflect.DeepEqual(frozen, payload) || action.Key != row.Dedup || row.Stream != "source-action:"+action.ChatID+":"+action.MessageID || action.Session == "" || action.MessageID == "" || action.ChatID == "" || (action.Operation != "react" && action.Operation != "delete") {
			return delivery.ErrInvalid
		}
		result = action
		return nil
	})
	return result, err
}
func (s *Store) CompleteSourceAction(ctx context.Context, j delivery.Job, accepted bool, status int) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "control-source-action" {
			return delivery.ErrInvalid
		}
		stored, err := q.SourceActionJob(ctx, db.SourceActionJobParams{AccountID: row.AccountID, JobID: row.ID})
		if err != nil {
			return err
		}
		var action sourceactions.Action
		if json.Unmarshal(stored.Payload, &action) != nil || action.Key != row.Dedup {
			return delivery.ErrInvalid
		}
		if action.State == "done" || action.State == "refused" {
			return nil
		}
		if action.State != "pending" {
			return delivery.ErrInvalid
		}
		action.State = "done"
		if !accepted {
			switch status {
			case 400, 403, 404, 413, 422, 501:
			default:
				return delivery.ErrInvalid
			}
			action.State = "refused"
			text := "This action was not applied on WhatsApp. The message may be unavailable or the provider may not permit this operation. Remaining Telegram content was kept."
			if _, err := metadataEnqueue(ctx, q, row.AccountID, row.Dedup+":notice", "notice:"+row.Dedup, "control-notice", control.Notice{Target: action.Target, Body: presentation.Plain(text), TTLSeconds: action.NoticeTTL}); err != nil {
				return err
			}
		}
		// Only an authoritative source revoke event admits Telegram deletion tasks.
		// A gateway acknowledgement may represent engine-specific delete-for-me.
		raw, _ := json.Marshal(action)
		return q.UpdateSourceAction(ctx, db.UpdateSourceActionParams{AccountID: row.AccountID, JobID: row.ID, Payload: raw})
	})
}

var _ sourceactions.Store = (*Store)(nil)
