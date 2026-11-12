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
	"github.com/veryshyjelly/bifrost/internal/revocations"
)

func (s *Store) PrepareRevocationCallback(ctx context.Context, j delivery.Job, c revocations.Settings) (control.Plan, error) {
	var result control.Plan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "tg" || !validRevocationSettings(c) {
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
		var envelope struct {
			Bot    string          `json:"bot"`
			Update json.RawMessage `json:"update"`
		}
		var u models.Update
		if json.Unmarshal(row.Payload, &envelope) != nil || envelope.Bot == "" || json.Unmarshal(envelope.Update, &u) != nil || u.CallbackQuery == nil || row.Dedup != "tg:"+envelope.Bot+":"+strconv.FormatInt(u.ID, 10) || !ingress.AuthorizedUpdate(envelope.Update, &u, c.OwnerID) {
			return delivery.ErrInvalid
		}
		callback := u.CallbackQuery
		parts := strings.Split(callback.Data, ":")
		if callback.From.ID != c.OwnerID || callback.From.IsBot || len(parts) != 2 || (parts[0] != "rev-delete" && parts[0] != "rev-keep") || parts[1] == "" {
			return delivery.ErrInvalid
		}
		p := control.Plan{Key: row.Dedup, Target: control.Target{BotID: envelope.Bot}, ReplyTTLSeconds: c.NoticeTTL, Answer: &bot.AnswerCallbackQueryParams{CallbackQueryID: callback.ID, Text: "This choice is no longer active."}}
		save := func() error {
			raw, _ := json.Marshal(p)
			if err := q.PrepareControl(ctx, db.PrepareControlParams{AccountID: row.AccountID, PlanKey: row.Dedup, JobID: row.ID, Payload: raw}); err != nil {
				return err
			}
			result = p
			return nil
		}
		m := callback.Message.Message
		if callback.Message.Type != models.MaybeInaccessibleMessageTypeMessage || m == nil {
			return save()
		}
		p.Target.ChatID = m.Chat.ID
		p.Target.ThreadID = int64(m.MessageThreadID)
		t, err := revocationTask(ctx, q, row.AccountID, parts[1])
		if errors.Is(err, pgx.ErrNoRows) {
			return save()
		}
		if err != nil {
			return err
		}
		expected := t.Primary.MessageID
		if t.Mode == "reply" {
			expected = t.PromptMessageID
		}
		if !t.Own || !t.Presented || t.OwnerID != c.OwnerID || t.Scope != changeBodyHash(row.AccountID) || t.Status != "pending" || t.ExpiresAt <= time.Now().UnixMilli() || t.NoticeBot != envelope.Bot || t.Primary.Target.ChatID != m.Chat.ID || t.Primary.Target.ThreadID != int64(m.MessageThreadID) || expected != int64(m.ID) {
			return save()
		}
		stage := "keep"
		t.Status = "keeping"
		p.Answer.Text = "Keeping the Telegram copy."
		if parts[0] == "rev-delete" {
			stage = "delete"
			t.Status = "deleting"
			p.Answer.Text = "Removing the Telegram copy…"
		}
		if err := saveRevocationTask(ctx, q, row.AccountID, t); err != nil {
			return err
		}
		if err := queueRevocation(ctx, q, row.AccountID, t, stage, t.Cursor); err != nil {
			return err
		}
		return save()
	})
	return result, err
}

var _ revocations.Store = (*Store)(nil)
