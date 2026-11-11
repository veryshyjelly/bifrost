package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func (s *Store) ControlContext(ctx context.Context, account, bot string, m *models.Message, management int64) (control.Context, error) {
	c := control.Context{Target: control.Target{BotID: bot, ChatID: m.Chat.ID}}
	if m.Chat.Type == models.ChatTypePrivate {
		return c, nil
	}
	r, err := s.queries.OutgoingRoute(ctx, db.OutgoingRouteParams{AccountID: account, TelegramChatID: m.Chat.ID, ThreadID: int64(m.MessageThreadID), ManagementChatID: management})
	if errors.Is(err, pgx.ErrNoRows) {
		if m.IsTopicMessage {
			c.Target.ThreadID = int64(m.MessageThreadID)
		}
		return c, nil
	}
	if err != nil {
		return c, err
	}
	c.Known = true
	c.Target.ThreadID = r.ThreadID
	c.Route = conversation.Route{ChatID: r.WhatsappChat, Name: r.Name, TelegramChatID: r.TelegramChatID, ThreadID: r.ThreadID, Mode: conversation.Mode(r.Mode), InboxRetired: r.InboxRetired}
	return c, nil
}
func decodeControl(row db.ControlPlanRow) (control.Plan, error) {
	var p control.Plan
	if json.Unmarshal(row.Payload, &p) != nil {
		return p, delivery.ErrInvalid
	}
	p.Complete = row.Complete
	return p, nil
}
func (s *Store) ControlPlan(ctx context.Context, account, key string) (control.Plan, bool, error) {
	r, err := s.queries.ControlPlan(ctx, db.ControlPlanParams{AccountID: account, PlanKey: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return control.Plan{}, false, nil
	}
	if err != nil {
		return control.Plan{}, false, err
	}
	p, err := decodeControl(r)
	return p, err == nil, err
}
func (s *Store) ControlAction(ctx context.Context, account, id string) (control.Action, bool, error) {
	r, err := s.queries.ControlAction(ctx, db.ControlActionParams{AccountID: account, ActionID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return control.Action{}, false, nil
	}
	if err != nil {
		return control.Action{}, false, err
	}
	var a control.Action
	if json.Unmarshal(r, &a) != nil || a.ID != id {
		return control.Action{}, false, delivery.ErrInvalid
	}
	return a, true, nil
}

func (s *Store) ControlWasCommand(ctx context.Context, account, bot string, chat, message int64) (bool, error) {
	return s.queries.ControlWasCommand(ctx, db.ControlWasCommandParams{AccountID: account, Scope: relayScope(bot, chat), ChatID: chat, MessageID: message})
}
func (s *Store) PrepareControl(ctx context.Context, job delivery.Job, p control.Plan) (control.Plan, error) {
	if p.Key != job.Dedup || p.Target.BotID == "" || (len(p.Packets) > 0 && p.Target.ChatID == 0) || p.Target.ThreadID < 0 || (p.Prefix != "" && p.Prefix != p.Key+":error" && p.Prefix != p.Key+":mention-error") || p.ReplyTTLSeconds < 5 || p.ReplyTTLSeconds > 86400 {
		return control.Plan{}, delivery.ErrInvalid
	}
	var result control.Plan
	err := s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		for i, packet := range p.Packets {
			target := p.PacketTarget(i)
			kinds := 0
			if packet.Markup != nil {
				kinds++
			}
			if packet.Keyboard != nil {
				kinds++
			}
			if packet.RemoveKeyboard {
				kinds++
			}
			if kinds > 1 || target.BotID != p.Target.BotID || target.ChatID == 0 || target.ThreadID < 0 {
				return delivery.ErrInvalid
			}
		}
		if row.Kind == "tg" {
			var source struct {
				Bot string `json:"bot"`
			}
			if json.Unmarshal(row.Payload, &source) != nil || source.Bot != p.Target.BotID {
				return delivery.ErrInvalid
			}
		} else if row.Kind == "control-clear" {
			var source struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(row.Payload, &source) != nil || source.ID == "" || p.Clear != nil || p.Command != nil || p.Scope != nil || p.Operator != nil || p.Start != nil || p.Mirror != nil || len(p.Actions) > 0 {
				return delivery.ErrInvalid
			}
			run, err := loadClear(ctx, q, row.AccountID, source.ID)
			if err != nil {
				return err
			}
			if p.Target != run.Target {
				return delivery.ErrInvalid
			}
		} else {
			return delivery.ErrInvalid
		}
		old, err := q.ControlPlan(ctx, db.ControlPlanParams{AccountID: row.AccountID, PlanKey: p.Key})
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
		if p.Operator != nil {
			if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
				return err
			}
			if err := queueOperator(ctx, q, row, p, p.Operator.ManagementChatID); err != nil {
				return err
			}
		}
		if p.Start != nil {
			if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
				return err
			}
			if err := prepareStart(ctx, q, row, &p); err != nil {
				return err
			}
		}
		if p.Mirror != nil {
			if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
				return err
			}
			if err := queueMirror(ctx, q, row, p); err != nil {
				return err
			}
		}
		if p.Scope != nil {
			if !p.Scope.Known || p.Scope.Target != p.Target {
				return delivery.ErrInvalid
			}
			r, err := q.LockControlRoute(ctx, db.LockControlRouteParams{AccountID: row.AccountID, WhatsappChat: p.Scope.Route.ChatID})
			if err != nil {
				return err
			}
			if r.TelegramChatID != p.Target.ChatID || r.ThreadID != p.Target.ThreadID || r.Mode == "muted" || r.Mode == "ignored" || r.InboxRetired != p.Scope.Route.InboxRetired {
				return delivery.ErrLocalRetry
			}
		}
		payload, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if err := q.PrepareControl(ctx, db.PrepareControlParams{AccountID: row.AccountID, PlanKey: p.Key, JobID: row.ID, Payload: payload}); err != nil {
			return err
		}
		for _, a := range p.Actions {
			if p.Scope == nil || a.ID == "" || len(a.ID) > 57 || a.Kind != "mention" || a.BotID != p.Target.BotID || a.ChatID != p.Target.ChatID || a.ThreadID != p.Target.ThreadID || a.WhatsAppChat != p.Scope.Route.ChatID || a.OwnerID <= 0 || a.Page < 1 || a.ExpiresAt <= 0 {
				return delivery.ErrInvalid
			}
			payload, err := json.Marshal(a)
			if err != nil {
				return err
			}
			if err := q.CreateControlAction(ctx, db.CreateControlActionParams{AccountID: row.AccountID, ActionID: a.ID, PlanKey: p.Key, Payload: payload}); err != nil {
				return err
			}
		}
		if c := p.Command; c != nil {
			if c.MessageID <= 0 || c.TTLSeconds < 5 || c.TTLSeconds > 86400 {
				return delivery.ErrInvalid
			}
			date := c.Date
			if date == 0 {
				date = time.Now().UnixMilli()
			}
			if err := q.ObserveControl(ctx, db.ObserveControlParams{AccountID: row.AccountID, Scope: relayScope(p.Target.BotID, p.Target.ChatID), BotID: p.Target.BotID, ChatID: p.Target.ChatID, MessageID: c.MessageID, ThreadID: p.Target.ThreadID, Kind: "command", MessageDate: date, Payload: row.Payload, ExpiresAt: time.Now().Add(time.Duration(c.TTLSeconds) * time.Second).UnixMilli()}); err != nil {
				return err
			}
			if err := q.ScheduleControlExpiry(ctx, db.ScheduleControlExpiryParams{AccountID: row.AccountID, Scope: relayScope(p.Target.BotID, p.Target.ChatID), ChatID: p.Target.ChatID, MessageID: c.MessageID}); err != nil {
				return err
			}
		}
		if err := queueClear(ctx, q, row.AccountID, p); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}

func recordControl(ctx context.Context, q *db.Queries, account string, p control.Plan, part int) error {
	if part < 0 || part >= len(p.Packets) {
		return delivery.ErrInvalid
	}
	target := p.PacketTarget(part)
	e, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: account, EffectKey: p.PacketKey(part)})
	if err != nil {
		return err
	}
	if e.State != "done" {
		return delivery.ErrIncomplete
	}
	var m models.Message
	if json.Unmarshal(e.Result, &m) != nil || m.ID <= 0 || m.Chat.ID != target.ChatID || (m.MessageThreadID != 0 && int64(m.MessageThreadID) != target.ThreadID && (target.ThreadID > 0 || m.IsTopicMessage)) {
		return delivery.ErrInvalid
	}
	date := int64(m.Date) * 1000
	if date == 0 {
		date = time.Now().UnixMilli()
	}
	if err := q.ObserveControl(ctx, db.ObserveControlParams{AccountID: account, Scope: relayScope(target.BotID, target.ChatID), BotID: target.BotID, ChatID: target.ChatID, MessageID: int64(m.ID), ThreadID: target.ThreadID, Kind: "log", MessageDate: date, Payload: e.Result, ExpiresAt: time.Now().Add(time.Duration(p.ReplyTTLSeconds) * time.Second).UnixMilli()}); err != nil {
		return err
	}
	return q.ScheduleControlExpiry(ctx, db.ScheduleControlExpiryParams{AccountID: account, Scope: relayScope(target.BotID, target.ChatID), ChatID: target.ChatID, MessageID: int64(m.ID)})
}
func (s *Store) RecordControl(ctx context.Context, job delivery.Job, key string, part int) error {
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		r, err := q.ControlPlan(ctx, db.ControlPlanParams{AccountID: row.AccountID, PlanKey: key})
		if err != nil {
			return err
		}
		if r.JobID != row.ID {
			return delivery.ErrInvalid
		}
		p, err := decodeControl(r)
		if err != nil {
			return err
		}
		return recordControl(ctx, q, row.AccountID, p, part)
	})
}
func (s *Store) CompleteControl(ctx context.Context, job delivery.Job, key string) error {
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		r, err := q.ControlPlan(ctx, db.ControlPlanParams{AccountID: row.AccountID, PlanKey: key})
		if err != nil {
			return err
		}
		if r.JobID != row.ID {
			return delivery.ErrInvalid
		}
		p, err := decodeControl(r)
		if err != nil {
			return err
		}
		if p.Answer != nil {
			e, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: p.Key + ":answer"})
			if err != nil {
				return err
			}
			if e.State != "done" && e.State != "discarded" {
				return delivery.ErrIncomplete
			}
		}
		for i := range p.Packets {
			if err := recordControl(ctx, q, row.AccountID, p, i); err != nil {
				return err
			}
		}
		return q.CompleteControl(ctx, db.CompleteControlParams{AccountID: row.AccountID, PlanKey: key})
	})
}

var _ control.Store = (*Store)(nil)
