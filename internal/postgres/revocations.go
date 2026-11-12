package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/revocations"
)

func validRevocationSettings(c revocations.Settings) bool {
	return c.Session != "" && c.OwnerID > 0 && c.ControlTTL >= 5 && c.ControlTTL <= 86400 && c.NoticeTTL >= 5 && c.NoticeTTL <= 86400
}
func revocationTask(ctx context.Context, q *db.Queries, account, id string) (revocations.Task, error) {
	row, err := q.RevocationTask(ctx, db.RevocationTaskParams{AccountID: account, TaskID: id})
	if err != nil {
		return revocations.Task{}, err
	}
	var t revocations.Task
	if json.Unmarshal(row.Payload, &t) != nil || t.ID != id || t.Scope != changeBodyHash(account) || t.ChatID != row.WhatsappChat || t.MessageID != row.MessageID || t.Cursor < 0 || t.Cursor > len(t.Copies) {
		return t, delivery.ErrInvalid
	}
	return t, nil
}
func saveRevocationTask(ctx context.Context, q *db.Queries, account string, t revocations.Task) error {
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return q.SaveRevocationTask(ctx, db.SaveRevocationTaskParams{AccountID: account, TaskID: t.ID, Payload: raw})
}
func queueRevocation(ctx context.Context, q *db.Queries, account string, t revocations.Task, stage string, cursor int) error {
	key := "revocation:" + t.ID + ":" + stage
	if stage == "delete" {
		key += ":" + strconv.Itoa(cursor)
	}
	stream := "revocation:" + t.ID
	due := time.Now().UnixMilli()
	if stage == "expire" {
		stream = "revocation-expire:" + t.ID
		due = t.ExpiresAt
	}
	raw, _ := json.Marshal(revocations.Job{ID: t.ID, Stage: stage, Cursor: cursor})
	return q.QueueRevocationJob(ctx, db.QueueRevocationJobParams{AccountID: account, Dedup: key, Stream: stream, Payload: raw, DueMs: float64(due)})
}
func revocationJob(ctx context.Context, q *db.Queries, row db.Job) (revocations.Job, revocations.Task, error) {
	var job revocations.Job
	if row.Kind != "control-revoke" || json.Unmarshal(row.Payload, &job) != nil || job.ID == "" || job.Cursor < 0 {
		return job, revocations.Task{}, delivery.ErrInvalid
	}
	t, err := revocationTask(ctx, q, row.AccountID, job.ID)
	if err != nil {
		return job, t, err
	}
	stream := "revocation:" + t.ID
	if job.Stage == "expire" {
		stream = "revocation-expire:" + t.ID
	}
	if row.Stream != stream {
		return job, t, delivery.ErrInvalid
	}
	return job, t, nil
}
func (s *Store) AdmitRevocation(ctx context.Context, j delivery.Job, c revocations.Settings) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if !validRevocationSettings(c) || row.Kind != "wa" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		var hook struct {
			Event   sdk.WebhookEvent `json:"event"`
			Account string           `json:"accountId"`
			Session string           `json:"sessionId"`
			Key     string           `json:"idempotencyKey"`
			Data    struct {
				ChatID    string `json:"chatId"`
				RevokedID string `json:"revokedId"`
				MessageID string `json:"messageId"`
				ID        string `json:"id"`
			} `json:"data"`
		}
		if json.Unmarshal(row.Payload, &hook) != nil || hook.Event != sdk.EventMessageRevoked || hook.Account != row.AccountID || hook.Session != c.Session || row.Dedup != "ow:"+hook.Key || hook.Data.ChatID == "" {
			return delivery.ErrInvalid
		}
		id := hook.Data.RevokedID
		if id == "" {
			id = hook.Data.MessageID
		}
		if id == "" {
			id = hook.Data.ID
		}
		if id == "" {
			return delivery.ErrInvalid
		}
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		chat, err := conversationKey(ctx, q, row.AccountID, hook.Data.ChatID, d)
		if err != nil {
			return err
		}
		if row.Stream != "wa:"+chat {
			return delivery.ErrInvalid
		}
		if err := q.SetMessageRevoked(ctx, db.SetMessageRevokedParams{AccountID: row.AccountID, WhatsappChat: chat, MessageID: id}); err != nil {
			return err
		}
		own, err := q.SourceMessageIsOwn(ctx, db.SourceMessageIsOwnParams{AccountID: row.AccountID, WhatsappChat: chat, MessageID: id})
		if err != nil {
			return err
		}
		copies, err := q.RetainedSourceMappings(ctx, db.RetainedSourceMappingsParams{AccountID: row.AccountID, WhatsappChat: chat, WhatsappMessageID: id})
		if err != nil {
			return err
		}
		groups := map[string][]revocations.Copy{}
		for _, copy := range copies {
			var receipt models.Message
			if json.Unmarshal(copy.Receipt, &receipt) != nil {
				return delivery.ErrInvalid
			}
			media := len(receipt.Photo) > 0 || receipt.Video != nil || receipt.Animation != nil || receipt.Audio != nil || receipt.Voice != nil || receipt.Document != nil || receipt.Sticker != nil
			var plan struct{ SourceAsset json.RawMessage }
			if json.Unmarshal(copy.PlanPayload, &plan) == nil && len(plan.SourceAsset) > 0 && string(plan.SourceAsset) != "null" {
				media = true
			}
			key := fmt.Sprintf("%s:%d:%d", copy.Scope, copy.TelegramChatID, copy.ThreadID)
			groups[key] = append(groups[key], revocations.Copy{Target: control.Target{BotID: copy.BotID, ChatID: copy.TelegramChatID, ThreadID: copy.ThreadID}, MessageID: copy.TelegramMessageID, Media: media})
		}
		keys := make([]string, 0, len(groups))
		for key := range groups {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		scope := changeBodyHash(row.AccountID)
		for _, key := range keys {
			taskID := changeBodyHash(scope + ":" + chat + ":" + id + ":" + key)[:24]
			if _, err := revocationTask(ctx, q, row.AccountID, taskID); err == nil {
				continue
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			copies := groups[key]
			primary := copies[0]
			for _, copy := range copies {
				if copy.Media {
					primary = copy
					break
				}
			}
			status, stage := "deleting", "delete"
			if own.Bool {
				status, stage = "pending", "present"
			}
			task := revocations.Task{ID: taskID, Scope: scope, SourceSession: c.Session, ChatID: chat, MessageID: id, Status: status, Mode: "inline", NoticeBot: primary.Target.BotID, OwnerID: c.OwnerID, ExpiresAt: time.Now().Add(time.Duration(c.ControlTTL) * time.Second).UnixMilli(), Own: own.Bool, NoticeTTL: c.NoticeTTL, Primary: primary, Copies: copies}
			raw, _ := json.Marshal(task)
			if err := q.CreateRevocationTask(ctx, db.CreateRevocationTaskParams{AccountID: row.AccountID, TaskID: task.ID, WhatsappChat: chat, MessageID: id, Payload: raw}); err != nil {
				return err
			}
			if err := queueRevocation(ctx, q, row.AccountID, task, stage, 0); err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *Store) RevocationStep(ctx context.Context, j delivery.Job, c revocations.Settings) (revocations.Task, bool, error) {
	var result revocations.Task
	active := false
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if !validRevocationSettings(c) {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		job, t, err := revocationJob(ctx, q, row)
		if err != nil {
			return err
		}
		if t.OwnerID != c.OwnerID {
			return delivery.ErrInvalid
		}
		result = t
		switch job.Stage {
		case "present":
			active = t.Own && t.Status == "pending" && !t.Presented
		case "delete":
			active = t.Status == "deleting" && job.Cursor == t.Cursor
		case "keep":
			active = t.Status == "keeping"
		case "expire":
			active = t.Own && (t.Status == "pending" || t.Status == "keeping") && t.Presented
		default:
			return delivery.ErrInvalid
		}
		return nil
	})
	return result, active, err
}
func (s *Store) UseRevocationReply(ctx context.Context, j delivery.Job) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		job, t, err := revocationJob(ctx, q, row)
		if err != nil {
			return err
		}
		if job.Stage != "present" || t.Status != "pending" || t.Presented || !t.Own {
			return delivery.ErrInvalid
		}
		t.Mode = "reply"
		t.NoticeBot = t.Primary.Target.BotID
		if t.Primary.Target.ChatID < 0 {
			t.NoticeBot = "master"
		}
		return saveRevocationTask(ctx, q, row.AccountID, t)
	})
}
func (s *Store) RevocationPrompt(ctx context.Context, j delivery.Job) (control.Plan, error) {
	var result control.Plan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		job, t, err := revocationJob(ctx, q, row)
		if err != nil {
			return err
		}
		if job.Stage != "present" || t.Status != "pending" || t.Mode != "reply" || t.Presented || !t.Own {
			return delivery.ErrInvalid
		}
		key := row.Dedup + ":prompt"
		if old, err := q.ControlPlan(ctx, db.ControlPlanParams{AccountID: row.AccountID, PlanKey: key}); err == nil {
			if old.JobID != row.ID {
				return delivery.ErrInvalid
			}
			result, err = decodeControl(old)
			return err
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		ttl := int(max(int64(5), (t.ExpiresAt-time.Now().UnixMilli()+999)/1000))
		p := control.Plan{Key: key, Target: control.Target{BotID: t.NoticeBot, ChatID: t.Primary.Target.ChatID, ThreadID: t.Primary.Target.ThreadID}, ReplyTTLSeconds: ttl, Packets: []control.Packet{{Text: "Your message was deleted on WhatsApp. Delete this Telegram copy or keep it?", Markup: t.Markup(), ReplyTo: t.Primary.MessageID, StrictReply: true}}}
		raw, _ := json.Marshal(p)
		if err := q.PrepareControl(ctx, db.PrepareControlParams{AccountID: row.AccountID, PlanKey: key, JobID: row.ID, Payload: raw}); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}
func (s *Store) MarkRevocationPresented(ctx context.Context, j delivery.Job) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		job, t, err := revocationJob(ctx, q, row)
		if err != nil {
			return err
		}
		if job.Stage != "present" || !t.Own || t.Status != "pending" {
			return delivery.ErrInvalid
		}
		if t.Presented {
			return nil
		}
		if t.Mode == "reply" {
			effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: row.Dedup + ":prompt:text:0"})
			if err != nil {
				return err
			}
			var message models.Message
			if effect.JobID != row.ID || effect.State != "done" || json.Unmarshal(effect.Result, &message) != nil || message.ID <= 0 || message.Chat.ID != t.Primary.Target.ChatID {
				return delivery.ErrUncertain
			}
			t.PromptMessageID = int64(message.ID)
		} else if t.Mode != "inline" {
			return delivery.ErrInvalid
		}
		t.Presented = true
		if err := saveRevocationTask(ctx, q, row.AccountID, t); err != nil {
			return err
		}
		return queueRevocation(ctx, q, row.AccountID, t, "expire", 0)
	})
}
func (s *Store) RefuseRevocationPrompt(ctx context.Context, j delivery.Job) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		job, t, err := revocationJob(ctx, q, row)
		if err != nil {
			return err
		}
		if job.Stage != "present" || !t.Own || t.Status != "pending" || t.Mode != "reply" {
			return delivery.ErrInvalid
		}
		effectKey := row.Dedup + ":prompt:text:0"
		effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: effectKey})
		if err != nil {
			return err
		}
		if effect.JobID != row.ID || effect.State != "pending" {
			return delivery.ErrUncertain
		}
		if _, err := q.DiscardEffect(ctx, db.DiscardEffectParams{AccountID: row.AccountID, JobID: row.ID, EffectKey: effectKey}); err != nil {
			return err
		}
		t.Status = "gone"
		return saveRevocationTask(ctx, q, row.AccountID, t)
	})
}
func (s *Store) AdvanceRevocationDelete(ctx context.Context, j delivery.Job, removed bool) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		job, t, err := revocationJob(ctx, q, row)
		if err != nil {
			return err
		}
		if job.Stage != "delete" || t.Status != "deleting" || job.Cursor != t.Cursor || t.Cursor >= len(t.Copies) {
			return delivery.ErrInvalid
		}
		copy := t.Copies[t.Cursor]
		if removed {
			scope := relayScope(copy.Target.BotID, copy.Target.ChatID)
			if err := q.MarkRevokedCopy(ctx, db.MarkRevokedCopyParams{AccountID: row.AccountID, Scope: scope, TelegramChatID: copy.Target.ChatID, TelegramMessageID: copy.MessageID}); err != nil {
				return err
			}
			if err := q.MarkRevokedObservation(ctx, db.MarkRevokedObservationParams{AccountID: row.AccountID, Scope: scope, ChatID: copy.Target.ChatID, MessageID: copy.MessageID}); err != nil {
				return err
			}
		} else {
			t.Blocked++
		}
		t.Cursor++
		if err := saveRevocationTask(ctx, q, row.AccountID, t); err != nil {
			return err
		}
		return queueRevocation(ctx, q, row.AccountID, t, "delete", t.Cursor)
	})
}
func (s *Store) BeginRevocationExpiry(ctx context.Context, j delivery.Job) (bool, error) {
	accepted := false
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		job, t, err := revocationJob(ctx, q, row)
		if err != nil {
			return err
		}
		if job.Stage != "expire" {
			return delivery.ErrInvalid
		}
		if t.Status != "pending" && t.Status != "keeping" {
			return nil
		}
		if t.ExpiresAt > time.Now().UnixMilli() {
			return control.WaitUntil{Deadline: time.UnixMilli(t.ExpiresAt)}
		}
		t.Status = "keeping"
		if err := saveRevocationTask(ctx, q, row.AccountID, t); err != nil {
			return err
		}
		accepted = true
		return nil
	})
	return accepted, err
}
func (s *Store) FinishRevocation(ctx context.Context, j delivery.Job, promptRemoved bool) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		_, t, err := revocationJob(ctx, q, row)
		if err != nil {
			return err
		}
		switch t.Status {
		case "keeping":
			t.Status = "kept"
		case "deleting":
			if t.Cursor < len(t.Copies) {
				return delivery.ErrIncomplete
			}
			t.Status = "deleted"
			if t.Blocked > 0 {
				t.Status = "partial"
				target := t.Primary.Target
				target.BotID = t.NoticeBot
				notice := control.Notice{Target: target, Body: presentation.Plain("Telegram could not remove every copy because of its deletion limits or permissions. Remaining content was kept."), TTLSeconds: t.NoticeTTL}
				if _, err := metadataEnqueue(ctx, q, row.AccountID, "revocation-limit:"+t.ID, "notice:"+t.ID, "control-notice", notice); err != nil {
					return err
				}
			}
		case "kept", "deleted", "partial", "gone":
			return nil
		default:
			return delivery.ErrInvalid
		}
		if promptRemoved && t.Mode == "reply" && t.PromptMessageID > 0 {
			if err := q.MarkRevokedObservation(ctx, db.MarkRevokedObservationParams{AccountID: row.AccountID, Scope: relayScope(t.NoticeBot, t.Primary.Target.ChatID), ChatID: t.Primary.Target.ChatID, MessageID: t.PromptMessageID}); err != nil {
				return err
			}
		}
		return saveRevocationTask(ctx, q, row.AccountID, t)
	})
}
