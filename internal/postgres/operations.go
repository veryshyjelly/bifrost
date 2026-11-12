package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/metadata"
	"github.com/veryshyjelly/bifrost/internal/operations"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/presentation"
)

func operationStatus(ctx context.Context, q *db.Queries, account string, c operations.Settings) (operations.Status, error) {
	result := operations.Status{Configured: c.Configured, Jobs: []operations.JobCount{}}
	counts, err := q.OperationCounts(ctx, account)
	if err != nil {
		return result, err
	}
	result.Bots, result.Conversations = counts.Bots, counts.Conversations
	result.MetadataSync.Cron, result.MetadataSync.Timezone = c.SyncCron, c.SyncTimezone
	result.TelegramControls.CommandTTLSeconds, result.TelegramControls.ButtonTTLSeconds, result.TelegramControls.CleanupProblems = c.CommandTTL, c.ControlTTL, counts.CleanupProblems
	if raw, err := q.OperationLastSync(ctx, account); err == nil {
		var r metadata.Report
		if json.Unmarshal(raw, &r) != nil {
			return result, delivery.ErrInvalid
		}
		result.MetadataSync.Last = &operations.LastSync{Run: r.RunID, FinishedAt: r.FinishedAt, Source: r.Source, Targets: r.Targets, Groups: r.Groups, Contacts: r.Contacts, Changed: r.Changed, Warnings: r.Warnings, MissingBots: r.MissingBots, SkippedPhotos: r.SkippedPhotos}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	jobs, err := q.OperationJobCounts(ctx, account)
	if err != nil {
		return result, err
	}
	for _, j := range jobs {
		result.Jobs = append(result.Jobs, operations.JobCount{State: j.State, Count: j.Count})
	}
	return result, nil
}

func (s *Store) PrepareOperationCommand(ctx context.Context, j delivery.Job, c operations.Settings, forum *bool) (control.Plan, error) {
	var result control.Plan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "tg" || !operations.ValidSettings(c) {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if p, found, err := existingGroupPlan(ctx, q, row); err != nil {
			return err
		} else if found {
			result = p
			return nil
		}
		var envelope struct {
			Bot    string
			Update json.RawMessage
		}
		var u models.Update
		if json.Unmarshal(row.Payload, &envelope) != nil || envelope.Bot == "" || json.Unmarshal(envelope.Update, &u) != nil || u.Message == nil || u.EditedMessage != nil || row.Dedup != "tg:"+envelope.Bot+":"+strconv.FormatInt(u.ID, 10) || !ingress.AuthorizedUpdate(envelope.Update, &u, c.OwnerID) {
			return delivery.ErrInvalid
		}
		m := u.Message
		parsed, ok := control.ParseCommand(m)
		if !ok || (parsed.Name != "status" && parsed.Name != "inbox") || parsed.Name == "inbox" && envelope.Bot != "master" || m.From == nil || m.From.IsBot || m.From.ID != c.OwnerID || m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != c.OwnerID || envelope.Bot != "master" && m.Chat.Type != models.ChatTypePrivate && parsed.Recipient == "" {
			return delivery.ErrInvalid
		}
		current, err := operatorContext(ctx, q, row.AccountID, envelope.Bot, m, c.Management)
		if err != nil {
			return err
		}
		p := control.Plan{Key: row.Dedup, Target: current.Target, ReplyTTLSeconds: c.NoticeTTL, Command: &control.Command{MessageID: int64(m.ID), Date: int64(m.Date) * 1000, TTLSeconds: c.CommandTTL}}
		text := ""
		if parsed.Name == "inbox" {
			if forum == nil {
				return delivery.ErrInvalid
			}
			text = "Management inbox connected. Incoming WhatsApp conversations will get separate topics."
			if !*forum || m.Chat.ID != c.Management {
				text = fmt.Sprintf("Set TELEGRAM_MANAGEMENT_CHAT_ID=%d in bridge.env and enable Topics in this private group, then restart the bridge.", m.Chat.ID)
			}
		} else {
			status, err := operationStatus(ctx, q, row.AccountID, c)
			if err != nil {
				return err
			}
			raw, err := json.MarshalIndent(status, "", "  ")
			if err != nil {
				return err
			}
			text = string(raw)
		}
		parts, err := presentation.Split(presentation.Plain(text), 3800)
		if err != nil {
			return err
		}
		for _, part := range parts {
			p.Packets = append(p.Packets, control.Packet{Text: part.Text, Entities: part.Entities})
		}
		if err := saveGroupPlan(ctx, q, row, p); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}

func operationManagement(ctx context.Context, q *db.Queries, account, key, kind string, data any) error {
	payload, _ := json.Marshal(map[string]any{"id": key, "type": kind, "data": data, "at": time.Now().UnixMilli()})
	_, err := q.Enqueue(ctx, db.EnqueueParams{AccountID: account, Dedup: "management:" + key, Stream: "management:" + key, Kind: "management", Payload: payload})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func (s *Store) QueueOperationSessionEvent(ctx context.Context, j delivery.Job, c operations.Settings) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		var hook sdk.WebhookDelivery
		var source struct {
			AccountID string `json:"accountId"`
		}
		if row.Kind != "wa" || !operations.ValidSettings(c) || json.Unmarshal(row.Payload, &hook) != nil || json.Unmarshal(row.Payload, &source) != nil || source.AccountID != row.AccountID || hook.SessionID != c.Session || hook.IdempotencyKey == "" || row.Dedup != "ow:"+hook.IdempotencyKey {
			return delivery.ErrInvalid
		}
		switch hook.Event {
		case sdk.EventSessionStatus, sdk.EventSessionDisconnected, sdk.EventMessageFailed:
		default:
			return delivery.ErrUnsupported
		}
		var data struct{ Status string }
		if json.Unmarshal(hook.Data, &data) != nil {
			return delivery.ErrInvalid
		}
		if data.Status == "" {
			data.Status = "changed"
		}
		return operationManagement(ctx, q, row.AccountID, row.Dedup, "session_status", map[string]any{"event": hook.Event, "status": data.Status})
	})
}

func (s *Store) PrepareOperationNotice(ctx context.Context, j delivery.Job, c operations.Settings) (control.Plan, error) {
	var result control.Plan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "management" || !operations.ValidSettings(c) {
			return delivery.ErrInvalid
		}
		if p, found, err := existingGroupPlan(ctx, q, row); err != nil {
			return err
		} else if found {
			result = p
			return nil
		}
		var event struct {
			Type string
			Data struct {
				Job                         json.RawMessage
				State, Error, Event, Status string
			}
		}
		if json.Unmarshal(row.Payload, &event) != nil {
			return delivery.ErrInvalid
		}
		text := ""
		switch event.Type {
		case "session_status":
			text = "OpenWA: " + event.Data.Event + " · " + event.Data.Status
		case "delivery_problem":
			id := ""
			if json.Unmarshal(event.Data.Job, &id) != nil {
				var n json.Number
				if json.Unmarshal(event.Data.Job, &n) != nil {
					return delivery.ErrInvalid
				}
				id = string(n)
			}
			message := event.Data.Error
			if message == "" {
				message = "review required"
			}
			text = "Delivery job " + id + " is " + event.Data.State + ": " + message + ". Use /status. Review it with the local bridge CLI before retrying."
		default:
			return delivery.ErrUnsupported
		}
		p := control.Plan{Key: row.Dedup, Target: control.Target{BotID: "master", ChatID: c.Management}, ReplyTTLSeconds: c.NoticeTTL}
		parts, err := presentation.Split(presentation.Plain(text), 3800)
		if err != nil {
			return err
		}
		for _, part := range parts {
			p.Packets = append(p.Packets, control.Packet{Text: part.Text, Entities: part.Entities})
		}
		if err := saveGroupPlan(ctx, q, row, p); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}

func (s *Store) ConfigureOperationNotices(ctx context.Context, account string, c operations.Settings) error {
	if account == "" || !operations.ValidSettings(c) {
		return delivery.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	q := s.queries.WithTx(tx)
	identity, err := q.Identity(ctx)
	if err != nil {
		return err
	}
	if identity.AccountID != account {
		return delivery.ErrInvalid
	}
	raw, _ := json.Marshal(c)
	if err := q.SaveOperationNoticeSettings(ctx, db.SaveOperationNoticeSettingsParams{AccountID: account, Payload: raw}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// queueOperationFailure belongs to the fenced job-state transaction. Default
// diagnostic stores have no settings row and never admit operational notices.
func queueOperationFailure(ctx context.Context, q *db.Queries, row db.Job, state delivery.State) error {
	if state == delivery.Done || row.Kind == "n8n" || strings.HasPrefix(row.Kind, "control-") {
		return nil
	}
	if row.Kind == "management" {
		var event struct{ Type string }
		if json.Unmarshal(row.Payload, &event) == nil && (event.Type == "delivery_problem" || event.Type == "session_status") {
			return nil
		}
	}
	raw, err := q.OperationNoticeSettings(ctx, row.AccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var c operations.Settings
	if json.Unmarshal(raw, &c) != nil || !operations.ValidSettings(c) {
		return delivery.ErrInvalid
	}
	reason := "retry scheduled"
	if state == delivery.Uncertain {
		reason = "review required"
	} else if state == delivery.Failed {
		reason = "definitive failure"
	}
	key := "failure:" + strconv.FormatInt(row.ID, 10) + ":" + strconv.FormatInt(int64(row.Attempts), 10)
	return operationManagement(ctx, q, row.AccountID, key, "delivery_problem", map[string]any{"job": row.ID, "state": state, "error": reason})
}

var _ operations.Store = (*Store)(nil)
