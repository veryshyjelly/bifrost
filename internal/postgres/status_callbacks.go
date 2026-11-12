package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/statuses"
)

func statusPresented(ctx context.Context, q *db.Queries, row db.Job, proof string, a statuses.Control, m *models.Message) (bool, error) {
	if a.PresentationJob > 0 {
		edit, err := q.StatusControlEdit(ctx, db.StatusControlEditParams{AccountID: row.AccountID, JobID: a.PresentationJob})
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		var p statuses.Edit
		if json.Unmarshal(edit.Payload, &p) != nil {
			return false, delivery.ErrInvalid
		}
		if !edit.Complete || !p.Presented || p.MessageID != int64(m.ID) || p.Target != (control.Target{BotID: "master", ChatID: m.Chat.ID, ThreadID: int64(m.MessageThreadID)}) {
			return false, nil
		}
	}
	stored, err := q.ControlPlan(ctx, db.ControlPlanParams{AccountID: row.AccountID, PlanKey: proof})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	p, err := decodeControl(stored)
	if err != nil {
		return false, err
	}
	for i := range p.Packets {
		target := p.PacketTarget(i)
		if target.BotID != "master" || target.ChatID != m.Chat.ID || target.ThreadID != int64(m.MessageThreadID) {
			continue
		}
		effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: p.PacketKey(i)})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		var receipt models.Message
		if effect.State != "done" || effect.JobID != stored.JobID || json.Unmarshal(effect.Result, &receipt) != nil || receipt.ID != m.ID || receipt.Chat.ID != m.Chat.ID {
			continue
		}
		observation, err := q.CleanupObservation(ctx, db.CleanupObservationParams{AccountID: row.AccountID, Scope: relayScope("master", m.Chat.ID), ChatID: m.Chat.ID, MessageID: int64(m.ID)})
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return !observation.Deleted && observation.ThreadID == int64(m.MessageThreadID), nil
	}
	return false, nil
}

func (s *Store) PrepareStatusCallback(ctx context.Context, j delivery.Job, c statuses.Settings) (control.Plan, error) {
	var result control.Plan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "tg" || !statuses.ValidSettings(c) {
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
		if json.Unmarshal(row.Payload, &envelope) != nil || json.Unmarshal(envelope.Update, &u) != nil || envelope.Bot == "" || u.CallbackQuery == nil || row.Dedup != "tg:"+envelope.Bot+":"+strconv.FormatInt(u.ID, 10) || !ingress.AuthorizedUpdate(envelope.Update, &u, c.OwnerID) {
			return delivery.ErrInvalid
		}
		callback := u.CallbackQuery
		if callback.ID == "" || callback.From.IsBot || callback.From.ID != c.OwnerID || !strings.HasPrefix(callback.Data, "statusctl:") {
			return delivery.ErrInvalid
		}
		p := control.Plan{Key: row.Dedup, Target: control.Target{BotID: envelope.Bot}, Answer: &bot.AnswerCallbackQueryParams{CallbackQueryID: callback.ID, Text: "This control expired. Use /muted_statuses."}, ReplyTTLSeconds: c.NoticeTTL}
		save := func() error {
			if err := saveGroupPlan(ctx, q, row, p); err != nil {
				return err
			}
			result = p
			return nil
		}
		m := callback.Message.Message
		if envelope.Bot != "master" || callback.Message.Type != models.MaybeInaccessibleMessageTypeMessage || m == nil {
			return save()
		}
		p.Target.ChatID, p.Target.ThreadID = m.Chat.ID, int64(m.MessageThreadID)
		if m.Chat.ID != c.Management {
			current, err := operatorContext(ctx, q, row.AccountID, "master", m, c.Management)
			if err != nil {
				return err
			}
			p.Target = current.Target
		}
		stored, err := q.StatusControl(ctx, db.StatusControlParams{AccountID: row.AccountID, ControlID: strings.TrimPrefix(callback.Data, "statusctl:")})
		if errors.Is(err, pgx.ErrNoRows) {
			return save()
		}
		if err != nil {
			return err
		}
		var a statuses.Control
		if json.Unmarshal(stored.Payload, &a) != nil || "statusctl:"+a.ID != callback.Data {
			return delivery.ErrInvalid
		}
		if a.OwnerID != c.OwnerID || a.ExpiresAt <= time.Now().UnixMilli() || a.ChatID != p.Target.ChatID || a.ThreadID != p.Target.ThreadID || (a.Kind != "unmute" && a.Kind != "page") {
			return save()
		}
		effective := *m
		effective.MessageThreadID = int(p.Target.ThreadID)
		presented, err := statusPresented(ctx, q, row, stored.PlanKey, a, &effective)
		if err != nil {
			return err
		}
		if !presented {
			return save()
		}
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		var mute statuses.Mute
		if a.Kind == "unmute" {
			current, err := statusMutes(ctx, q, row.AccountID, d)
			if err != nil {
				return err
			}
			previous, ok := current[d.Canonical(a.Author)]
			if !ok || !previous.Active || previous.Revision != a.Revision {
				p.Answer.Text = "That mute has changed. Use /muted_statuses for the current list."
				return save()
			}
			mute, err = setStatusMute(ctx, q, row, d, a.Author, previous.Name, false)
			if err != nil {
				return err
			}
		}
		p.Answer.Text = ""
		if a.Kind == "unmute" {
			p.Answer.Text = "Unmuted this person’s Status posts."
		}
		child, err := metadataEnqueue(ctx, q, row.AccountID, row.Dedup+":status-edit", "status-edit:"+strconv.FormatInt(m.Chat.ID, 10)+":"+strconv.Itoa(m.ID), "control-status-edit", map[string]any{"Source": row.Dedup})
		if err != nil {
			return err
		}
		edit := statuses.Edit{Target: p.Target, MessageID: int64(m.ID), Markup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}}}
		if a.Kind == "page" || a.View == "list" {
			edit.Body, edit.Markup, err = statusPage(ctx, q, row, d, stored.PlanKey, c, p.Target, a.Page, child)
			if err != nil {
				return err
			}
		} else {
			edit.Body = presentation.Concat(presentation.Plain("Unmuted Status posts from "), presentation.Sender(statusName(d, mute), mute.Author), presentation.Plain(". New posts will appear again."))
		}
		raw, _ := json.Marshal(edit)
		if err := q.CreateStatusControlEdit(ctx, db.CreateStatusControlEditParams{AccountID: row.AccountID, JobID: child, Payload: raw}); err != nil {
			return err
		}
		return save()
	})
	return result, err
}

func (s *Store) CompleteStatusCallback(ctx context.Context, j delivery.Job) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "tg" {
			return delivery.ErrInvalid
		}
		var envelope struct{ Update models.Update }
		if json.Unmarshal(row.Payload, &envelope) != nil || envelope.Update.CallbackQuery == nil || !strings.HasPrefix(envelope.Update.CallbackQuery.Data, "statusctl:") {
			return delivery.ErrInvalid
		}
		r, err := q.ControlPlan(ctx, db.ControlPlanParams{AccountID: row.AccountID, PlanKey: row.Dedup})
		if err != nil {
			return err
		}
		p, err := decodeControl(r)
		if err != nil {
			return err
		}
		if r.JobID != row.ID || len(p.Packets) != 0 || p.Answer == nil || p.Answer.CallbackQueryID != envelope.Update.CallbackQuery.ID {
			return delivery.ErrInvalid
		}
		return q.CompleteControl(ctx, db.CompleteControlParams{AccountID: row.AccountID, PlanKey: row.Dedup})
	})
}
func (s *Store) StatusControlEdit(ctx context.Context, j delivery.Job) (statuses.Edit, error) {
	var result statuses.Edit
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "control-status-edit" {
			return delivery.ErrInvalid
		}
		r, err := q.StatusControlEdit(ctx, db.StatusControlEditParams{AccountID: row.AccountID, JobID: row.ID})
		if err != nil {
			return err
		}
		if json.Unmarshal(r.Payload, &result) != nil || result.Target.BotID != "master" || result.MessageID <= 0 || result.Body.Text == "" {
			return delivery.ErrInvalid
		}
		result.Complete = r.Complete
		return nil
	})
	return result, err
}
func (s *Store) CompleteStatusControlEdit(ctx context.Context, j delivery.Job, presented bool) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "control-status-edit" {
			return delivery.ErrInvalid
		}
		r, err := q.StatusControlEdit(ctx, db.StatusControlEditParams{AccountID: row.AccountID, JobID: row.ID})
		if err != nil {
			return err
		}
		if r.Complete {
			return nil
		}
		var p statuses.Edit
		if json.Unmarshal(r.Payload, &p) != nil {
			return delivery.ErrInvalid
		}
		p.Presented = presented
		raw, _ := json.Marshal(p)
		if err := q.UpdateStatusControlEdit(ctx, db.UpdateStatusControlEditParams{AccountID: row.AccountID, JobID: row.ID, Payload: raw}); err != nil {
			return err
		}
		return q.CompleteStatusControlEdit(ctx, db.CompleteStatusControlEditParams{AccountID: row.AccountID, JobID: row.ID})
	})
}

var _ statuses.ControlStore = (*Store)(nil)
