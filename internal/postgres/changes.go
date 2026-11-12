package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/changes"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

func changeBodyHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
func changePlan(ctx context.Context, q *db.Queries, account, key string) (db.MessageChangePlan, changes.Plan, error) {
	row, err := q.ChangePlan(ctx, db.ChangePlanParams{AccountID: account, PlanKey: key})
	if err != nil {
		return row, changes.Plan{}, err
	}
	var p changes.Plan
	if json.Unmarshal(row.Payload, &p) != nil || p.Key != key {
		return row, p, delivery.ErrInvalid
	}
	p.Complete = row.Complete
	return row, p, nil
}
func (s *Store) ChangePlan(ctx context.Context, account, key string) (changes.Plan, bool, error) {
	_, p, err := changePlan(ctx, s.queries, account, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, false, nil
	}
	return p, err == nil, err
}
func saveChange(ctx context.Context, q *db.Queries, account string, p changes.Plan) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return q.UpdateChange(ctx, db.UpdateChangeParams{AccountID: account, PlanKey: p.Key, Payload: raw})
}

func editInput(ctx context.Context, q *db.Queries, row db.Job, c changes.Settings) (changes.Input, error) {
	in := changes.Input{Session: c.Session}
	if row.Kind == "tg" {
		var envelope struct {
			Bot    string          `json:"bot"`
			Update json.RawMessage `json:"update"`
		}
		var u models.Update
		if json.Unmarshal(row.Payload, &envelope) != nil || envelope.Bot == "" || json.Unmarshal(envelope.Update, &u) != nil || u.EditedMessage == nil || row.Dedup != "tg:"+envelope.Bot+":"+strconv.FormatInt(u.ID, 10) {
			return in, delivery.ErrInvalid
		}
		if !ingress.AuthorizedUpdate(envelope.Update, &u, c.OwnerID) {
			return in, delivery.ErrInvalid
		}
		m := u.EditedMessage
		if m.From == nil || m.From.IsBot || m.From.ID != c.OwnerID || (envelope.Bot != "master" && m.Chat.Type != models.ChatTypePrivate) {
			return in, nil
		}
		if m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != c.OwnerID {
			return in, delivery.ErrInvalid
		}
		if _, command := control.ParseCommand(m); command {
			return in, nil
		}
		wasCommand, err := q.ControlWasCommand(ctx, db.ControlWasCommandParams{AccountID: row.AccountID, Scope: relayScope(envelope.Bot, m.Chat.ID), ChatID: m.Chat.ID, MessageID: int64(m.ID)})
		if err != nil {
			return in, err
		}
		if wasCommand {
			return in, nil
		}
		mapped, err := q.TelegramMapping(ctx, db.TelegramMappingParams{AccountID: row.AccountID, Scope: relayScope(envelope.Bot, m.Chat.ID), TelegramChatID: m.Chat.ID, TelegramMessageID: int64(m.ID)})
		if errors.Is(err, pgx.ErrNoRows) {
			return in, nil
		}
		if err != nil {
			return in, err
		}
		if mapped.Direction != "out" || mapped.Deleted || mapped.ObservationDeleted || mapped.ThreadID != int64(m.MessageThreadID) {
			return in, nil
		}
		if m.Chat.ID > 0 {
			contact, err := q.ContactForBot(ctx, db.ContactForBotParams{AccountID: row.AccountID, BotID: envelope.Bot})
			if errors.Is(err, pgx.ErrNoRows) {
				return in, nil
			}
			if err != nil {
				return in, err
			}
			d, err := directory(ctx, q, row.AccountID)
			if err != nil {
				return in, err
			}
			chat, err := conversationKey(ctx, q, row.AccountID, contact, d)
			if err != nil {
				return in, err
			}
			if chat != mapped.WhatsappChat {
				return in, nil
			}
		}
		route, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: mapped.WhatsappChat})
		if err != nil {
			return in, err
		}
		if route.Mode == "muted" || route.Mode == "ignored" {
			return in, nil
		}
		if m.Chat.ID < 0 && (route.TelegramChatID != m.Chat.ID || route.ThreadID != int64(m.MessageThreadID)) {
			return in, nil
		}
		in.Event = "telegram.edit"
		in.ChatID = mapped.WhatsappChat
		in.MessageID = mapped.WhatsappMessageID
		in.TelegramBot = envelope.Bot
		in.TelegramMessage = m
		return in, nil
	}
	if row.Kind != "wa" {
		return in, delivery.ErrInvalid
	}
	var hook struct {
		Event   sdk.WebhookEvent `json:"event"`
		Session string           `json:"sessionId"`
		Account string           `json:"accountId"`
		Key     string           `json:"idempotencyKey"`
		Data    json.RawMessage  `json:"data"`
	}
	if json.Unmarshal(row.Payload, &hook) != nil || hook.Event != sdk.EventMessageEdited || hook.Session != c.Session || hook.Account != row.AccountID || row.Dedup != "ow:"+hook.Key {
		return in, delivery.ErrInvalid
	}
	var data struct {
		ChatID       string    `json:"chatId"`
		MessageID    string    `json:"messageId"`
		ID           string    `json:"id"`
		Body         *string   `json:"body"`
		NewBody      *string   `json:"newBody"`
		Text         *string   `json:"text"`
		MentionedIDs *[]string `json:"mentionedIds"`
	}
	if json.Unmarshal(hook.Data, &data) != nil || data.ChatID == "" {
		return in, delivery.ErrInvalid
	}
	id := data.MessageID
	if id == "" {
		id = data.ID
	}
	if id == "" {
		return in, delivery.ErrInvalid
	}
	directory, err := directory(ctx, q, row.AccountID)
	if err != nil {
		return in, err
	}
	chat, err := conversationKey(ctx, q, row.AccountID, data.ChatID, directory)
	if err != nil {
		return in, err
	}
	revoked, err := q.HistoryMessageRevoked(ctx, db.HistoryMessageRevokedParams{AccountID: row.AccountID, WhatsappChat: chat, MessageID: id})
	if err != nil {
		return in, err
	}
	if revoked {
		return in, nil
	}
	body := ""
	for _, candidate := range []*string{data.Body, data.NewBody, data.Text} {
		if candidate != nil {
			body = *candidate
			break
		}
	}
	var mentioned json.RawMessage
	if data.MentionedIDs != nil {
		mentioned, _ = json.Marshal(*data.MentionedIDs)
	}
	if err := q.SaveLatestMessageEdit(ctx, db.SaveLatestMessageEditParams{AccountID: row.AccountID, WhatsappChat: chat, MessageID: id, Body: body, MentionedIds: mentioned, JobID: row.ID}); err != nil {
		return in, err
	}
	echo, err := q.OutboundEditEcho(ctx, db.OutboundEditEchoParams{AccountID: row.AccountID, WhatsappChat: chat, MessageID: id, BodyHash: changeBodyHash(body)})
	if err != nil {
		return in, err
	}
	if echo {
		return in, nil
	}
	in.Event = "whatsapp.edit"
	in.ChatID = chat
	in.MessageID = id
	in.Body = body
	if data.MentionedIDs != nil {
		in.MentionedIDs = *data.MentionedIDs
	} else {
		latest, err := q.LatestMessageEdit(ctx, db.LatestMessageEditParams{AccountID: row.AccountID, WhatsappChat: chat, MessageID: id})
		if err != nil {
			return in, err
		}
		if latest.MentionedIds != nil && json.Unmarshal(latest.MentionedIds, &in.MentionedIDs) != nil {
			return in, delivery.ErrInvalid
		}
	}
	mappings, err := q.RetainedSourceMappings(ctx, db.RetainedSourceMappingsParams{AccountID: row.AccountID, WhatsappChat: chat, WhatsappMessageID: id})
	if err != nil {
		return in, err
	}
	for _, mapping := range mappings {
		format := presentation.Options{Bot: mapping.BotID, Name: "WhatsApp", Sender: chat, Own: mapping.Direction == "out"}
		var shape struct{ Format string }
		json.Unmarshal(mapping.PlanPayload, &shape)
		if shape.Format == "native-packets-v1" {
			var p relay.NativePlan
			if json.Unmarshal(mapping.PlanPayload, &p) != nil {
				return in, delivery.ErrInvalid
			}
			format = p.Presentation
			if data.MentionedIDs == nil && in.MentionedIDs == nil {
				in.MentionedIDs = p.Source.MentionedIDs
			}
		} else {
			var p relay.Plan
			if json.Unmarshal(mapping.PlanPayload, &p) != nil {
				return in, delivery.ErrInvalid
			}
			if p.Presentation != nil {
				format = *p.Presentation
			}
		}
		var receipt models.Message
		if json.Unmarshal(mapping.Receipt, &receipt) != nil {
			return in, delivery.ErrInvalid
		}
		caption := len(receipt.Photo) > 0 || receipt.Video != nil || receipt.Animation != nil || receipt.Audio != nil || receipt.Voice != nil || receipt.Document != nil || receipt.Sticker != nil
		format.Bot = mapping.BotID
		in.Destinations = append(in.Destinations, changes.Destination{Seq: mapping.Seq, Target: control.Target{BotID: mapping.BotID, ChatID: mapping.TelegramChatID, ThreadID: mapping.ThreadID}, MessageID: mapping.TelegramMessageID, Caption: caption, Format: format})
	}
	return in, nil
}
func (s *Store) EditInput(ctx context.Context, j delivery.Job, c changes.Settings) (changes.Input, error) {
	var result changes.Input
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		var err error
		result, err = editInput(ctx, q, row, c)
		return err
	})
	return result, err
}
func (s *Store) PrepareChange(ctx context.Context, j delivery.Job, p changes.Plan, c changes.Settings) (changes.Plan, error) {
	var result changes.Plan
	if p.Key != j.Dedup || c.Session == "" || c.OwnerID <= 0 || c.Management >= 0 {
		return result, delivery.ErrInvalid
	}
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if owner, old, err := changePlan(ctx, q, row.AccountID, p.Key); err == nil {
			if owner.JobID != row.ID {
				return delivery.ErrInvalid
			}
			result = old
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		actual, err := editInput(ctx, q, row, c)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, p.Input) {
			return delivery.ErrLocalRetry
		}
		if p.Complete {
			return delivery.ErrInvalid
		}
		if actual.Event == "" && len(p.Steps) != 0 {
			return delivery.ErrInvalid
		}
		if actual.Event == "telegram.edit" && len(p.Steps) != 1 {
			return delivery.ErrInvalid
		}
		if actual.Event == "whatsapp.edit" && len(p.Steps) != len(actual.Destinations) {
			return delivery.ErrInvalid
		}
		for i, step := range p.Steps {
			if step.State != "pending" || step.Refusal == nil || step.Refusal.TTLSeconds != c.NoticeTTL {
				return delivery.ErrInvalid
			}
			if _, err := step.Refusal.Plan(p.Key); err != nil {
				return err
			}
			if actual.Event == "telegram.edit" {
				if step.Kind != "whatsapp.edit" || step.Edit == nil || step.Edit.ChatID != actual.ChatID || step.Edit.MessageID != actual.MessageID || step.Refusal.Target != (control.Target{BotID: actual.TelegramBot, ChatID: actual.TelegramMessage.Chat.ID, ThreadID: int64(actual.TelegramMessage.MessageThreadID)}) {
					return delivery.ErrInvalid
				}
			} else {
				kind := "telegram.text"
				if actual.Destinations[i].Caption {
					kind = "telegram.caption"
				}
				if !reflect.DeepEqual(step.Destination, actual.Destinations[i]) || step.Kind != kind {
					return delivery.ErrInvalid
				}
				target := step.Destination.Target
				if target.ChatID < 0 {
					target.BotID = "master"
				}
				if step.Refusal.Target != target || step.Refusal.ReplyTo != step.Destination.MessageID {
					return delivery.ErrInvalid
				}
			}
		}
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if err := q.PrepareChange(ctx, db.PrepareChangeParams{AccountID: row.AccountID, PlanKey: p.Key, JobID: row.ID, Payload: raw}); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}
func sameChangeRequest(a, b json.RawMessage) bool {
	var x, y changes.Step
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}
func (s *Store) RecordChange(ctx context.Context, j delivery.Job, key string, index int) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		owner, p, err := changePlan(ctx, q, row.AccountID, key)
		if err != nil {
			return err
		}
		if owner.JobID != row.ID {
			return delivery.ErrInvalid
		}
		effectKey, request, err := p.Request(index)
		if err != nil {
			return err
		}
		effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: effectKey})
		if err != nil {
			return err
		}
		if effect.JobID != row.ID || effect.State != "done" || !sameChangeRequest(effect.Request, request) {
			return delivery.ErrIncomplete
		}
		step := &p.Steps[index]
		if step.Kind == "whatsapp.edit" {
			var receipt sdk.MessageResponse
			if step.Edit == nil || json.Unmarshal(effect.Result, &receipt) != nil || receipt.MessageID != step.Edit.MessageID {
				return delivery.ErrUncertain
			}
			if err := q.SaveOutboundEdit(ctx, db.SaveOutboundEditParams{AccountID: row.AccountID, WhatsappChat: step.Edit.ChatID, MessageID: step.Edit.MessageID, BodyHash: changeBodyHash(step.Edit.Body)}); err != nil {
				return err
			}
		}
		step.State = "done"
		return saveChange(ctx, q, row.AccountID, p)
	})
}
func (s *Store) RefuseChange(ctx context.Context, j delivery.Job, key string, index, status int, reason string) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		owner, p, err := changePlan(ctx, q, row.AccountID, key)
		if err != nil {
			return err
		}
		if owner.JobID != row.ID {
			return delivery.ErrInvalid
		}
		effectKey, request, err := p.Request(index)
		if err != nil {
			return err
		}
		effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: effectKey})
		if err != nil {
			return err
		}
		step := &p.Steps[index]
		if step.State == "refused" && effect.State == "discarded" {
			return nil
		}
		if step.State != "pending" || effect.JobID != row.ID || effect.State != "pending" || !sameChangeRequest(effect.Request, request) {
			return delivery.ErrUncertain
		}
		if step.Kind == "whatsapp.edit" {
			switch status {
			case 400, 403, 404, 413, 422:
			default:
				return delivery.ErrInvalid
			}
		} else if status != 400 {
			return delivery.ErrInvalid
		}
		if _, err := q.DiscardEffect(ctx, db.DiscardEffectParams{AccountID: row.AccountID, JobID: row.ID, EffectKey: effectKey}); err != nil {
			return err
		}
		if step.Refusal != nil && (step.Kind == "whatsapp.edit" || (reason != "message_missing" && reason != "thread_missing")) {
			n := step.Refusal
			noticeKey := key + ":edit-notice:" + strconv.Itoa(index)
			stream := "notice:" + relayScope(n.Target.BotID, n.Target.ChatID) + ":" + strconv.FormatInt(n.Target.ChatID, 10) + ":" + strconv.FormatInt(n.Target.ThreadID, 10)
			if _, err := metadataEnqueue(ctx, q, row.AccountID, noticeKey, stream, "control-notice", n); err != nil {
				return err
			}
		}
		step.State = "refused"
		return saveChange(ctx, q, row.AccountID, p)
	})
}
func (s *Store) CompleteChange(ctx context.Context, j delivery.Job, key string) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		owner, p, err := changePlan(ctx, q, row.AccountID, key)
		if err != nil {
			return err
		}
		if owner.JobID != row.ID {
			return delivery.ErrInvalid
		}
		for _, step := range p.Steps {
			if step.State != "done" && step.State != "refused" {
				return delivery.ErrIncomplete
			}
		}
		count, err := q.UnfinishedEffects(ctx, row.ID)
		if err != nil {
			return err
		}
		if count != 0 {
			return delivery.ErrIncomplete
		}
		return q.CompleteChange(ctx, db.CompleteChangeParams{AccountID: row.AccountID, PlanKey: key})
	})
}

var _ changes.Store = (*Store)(nil)
