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

func queueMirror(ctx context.Context, q *db.Queries, row db.Job, p control.Plan) error {
	r := p.Mirror
	if r == nil {
		return nil
	}
	if row.Kind != "tg" || r.OwnerID <= 0 || r.ManagementChatID >= 0 || !strings.HasSuffix(r.ChatID, "@g.us") || p.Target.BotID != "master" || p.Command == nil || p.Operator != nil || p.Start != nil || p.Scope != nil || p.Clear != nil || p.Answer != nil || len(p.Actions) > 0 || len(p.Packets) > 0 {
		return delivery.ErrInvalid
	}
	var source struct {
		Bot    string          `json:"bot"`
		Update json.RawMessage `json:"update"`
	}
	var u models.Update
	if json.Unmarshal(row.Payload, &source) != nil || json.Unmarshal(source.Update, &u) != nil || !ingress.AuthorizedUpdate(source.Update, &u, r.OwnerID) || u.Message == nil || u.EditedMessage != nil || source.Bot != "master" || row.Dedup != "tg:master:"+strconv.FormatInt(u.ID, 10) || int64(u.Message.ID) != p.Command.MessageID {
		return delivery.ErrInvalid
	}
	m := u.Message
	parsed, ok := control.ParseCommand(m)
	if !ok || parsed.Name != r.Operation || (m.Chat.Type != models.ChatTypeGroup && m.Chat.Type != models.ChatTypeSupergroup) || m.Chat.ID == r.ManagementChatID {
		return delivery.ErrInvalid
	}
	c, err := operatorContext(ctx, q, row.AccountID, "master", m, r.ManagementChatID)
	if err != nil {
		return err
	}
	if c.Target != p.Target {
		return delivery.ErrLocalRetry
	}
	event := control.MirrorEvent{ID: p.Key, Type: "link_group", At: time.Now().UnixMilli()}
	event.Data.ChatID = r.ChatID
	event.Data.TelegramChat = m.Chat.ID
	switch r.Operation {
	case "link":
		if len(parsed.Args) == 0 || parsed.Args[0] != r.ChatID {
			return delivery.ErrInvalid
		}
	case "unmirror":
		if len(parsed.Args) > 0 || !c.Known || c.Route.ChatID != r.ChatID {
			return delivery.ErrLocalRetry
		}
		event.Type = "return_to_inbox"
		event.Data.PreviousReturn, err = q.MirrorReturnKey(ctx, db.MirrorReturnKeyParams{AccountID: row.AccountID, WhatsappChat: r.ChatID})
		if err != nil {
			return err
		}
	default:
		return delivery.ErrInvalid
	}
	b, _ := json.Marshal(event)
	return enqueueTransitionEvent(ctx, q, row.AccountID, "management:"+p.Key, b)
}
func enqueueTransitionEvent(ctx context.Context, q *db.Queries, account, key string, payload []byte) error {
	_, err := q.Enqueue(ctx, db.EnqueueParams{AccountID: account, Dedup: key, Stream: key, Kind: "management", Payload: payload})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}
func loadTransition(ctx context.Context, q *db.Queries, row db.Job) (control.Transition, error) {
	b, err := q.MirrorTransition(ctx, db.MirrorTransitionParams{AccountID: row.AccountID, JobID: row.ID})
	if err != nil {
		return control.Transition{}, err
	}
	var p control.Transition
	if json.Unmarshal(b, &p) != nil || p.ChatID == "" || p.Management >= 0 || p.FromChat >= 0 {
		return p, delivery.ErrInvalid
	}
	return p, nil
}
func saveTransition(ctx context.Context, q *db.Queries, row db.Job, p control.Transition, create bool) error {
	b, _ := json.Marshal(p)
	if create {
		return q.CreateMirrorTransition(ctx, db.CreateMirrorTransitionParams{AccountID: row.AccountID, JobID: row.ID, Payload: b})
	}
	return q.SaveMirrorTransition(ctx, db.SaveMirrorTransitionParams{AccountID: row.AccountID, JobID: row.ID, Payload: b})
}
func mirrorNotice(ctx context.Context, q *db.Queries, row db.Job, p control.Transition, text, suffix string, markup *models.InlineKeyboardMarkup) error {
	plan := control.Plan{Key: row.Dedup, Prefix: row.Dedup + suffix, Target: control.Target{BotID: "master", ChatID: p.FromChat}, Packets: []control.Packet{{Text: text, Markup: markup}}, ReplyTTLSeconds: p.TTL}
	if markup != nil {
		plan.ReplyTTLSeconds = p.ControlTTL
	}
	b, _ := json.Marshal(plan)
	return q.PrepareControl(ctx, db.PrepareControlParams{AccountID: row.AccountID, PlanKey: row.Dedup, JobID: row.ID, Payload: b})
}
func (s *Store) PrepareTransition(ctx context.Context, j delivery.Job, management, owner int64, ttl, controlTTL int) (control.Transition, error) {
	if management >= 0 || owner <= 0 || ttl < 5 || ttl > 86400 || controlTTL < 5 || controlTTL > 86400 {
		return control.Transition{}, delivery.ErrInvalid
	}
	var result control.Transition
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		var source control.MirrorEvent
		if row.Kind != "management" || json.Unmarshal(row.Payload, &source) != nil || (source.Type != "link_group" && source.Type != "return_to_inbox") || !strings.HasSuffix(source.Data.ChatID, "@g.us") || source.Data.TelegramChat >= 0 {
			return delivery.ErrInvalid
		}
		if source.Data.RequestID != "" {
			return delivery.ErrUnsupported
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		old, err := loadTransition(ctx, q, row)
		if err == nil {
			result = old
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// Current configuration constrains new plans. Existing plans retain
		// their original management chat, including after a role change.
		if source.Data.TelegramChat == management {
			return delivery.ErrInvalid
		}
		p := control.Transition{Kind: source.Type, ChatID: source.Data.ChatID, FromChat: source.Data.TelegramChat, Management: management, OwnerID: owner, TTL: ttl, ControlTTL: controlTTL, Stage: "link"}
		route, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: p.ChatID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		found := err == nil
		if !found && p.Kind == "link_group" {
			return delivery.ErrInvalid
		}
		p.Name = route.Name
		p.ExpectedChat = route.TelegramChatID
		p.ExpectedThread = route.ThreadID
		key, err := q.MirrorReturnKey(ctx, db.MirrorReturnKeyParams{AccountID: row.AccountID, WhatsappChat: p.ChatID})
		if err != nil {
			return err
		}
		p.PreviousReturn = key
		if p.Kind == "return_to_inbox" {
			text, suffix := "", ""
			if !found || route.TelegramChatID != p.FromChat || key != source.Data.PreviousReturn {
				text, suffix = control.UnmirrorStale, ":stale"
			} else {
				active, err := q.ActiveHistory(ctx, db.ActiveHistoryParams{AccountID: row.AccountID, WhatsappChat: p.ChatID})
				if err != nil {
					return err
				}
				if active {
					text, suffix = control.UnmirrorBusy, ":busy"
				}
			}
			if text != "" {
				p.Stage = "notice"
				if err := mirrorNotice(ctx, q, row, p, text, suffix, nil); err != nil {
					return err
				}
			} else {
				busy, err := q.RouteTransitionBusy(ctx, db.RouteTransitionBusyParams{AccountID: row.AccountID, WhatsappChat: p.ChatID, JobID: row.ID})
				if err != nil {
					return err
				}
				if busy {
					return delivery.ErrLocalRetry
				}
				if err := q.LockRouteTransition(ctx, db.LockRouteTransitionParams{AccountID: row.AccountID, WhatsappChat: p.ChatID, JobID: row.ID}); err != nil {
					return err
				}
				p.Stage = "create"
			}
		}
		if err := saveTransition(ctx, q, row, p, true); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}
func transitionRoute(ctx context.Context, q *db.Queries, row db.Job, p control.Transition) (db.ConversationRouteRow, error) {
	r, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: p.ChatID})
	if err != nil {
		return r, err
	}
	key, err := q.MirrorReturnKey(ctx, db.MirrorReturnKeyParams{AccountID: row.AccountID, WhatsappChat: p.ChatID})
	if err != nil {
		return r, err
	}
	if r.TelegramChatID != p.ExpectedChat || r.ThreadID != p.ExpectedThread || key != p.PreviousReturn {
		return r, delivery.ErrInvalid
	}
	return r, nil
}
func (s *Store) CommitMirrorLink(ctx context.Context, j delivery.Job) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, err := loadTransition(ctx, q, row)
		if err != nil {
			return err
		}
		if p.Kind != "link_group" {
			return delivery.ErrInvalid
		}
		if p.Stage == "linked" {
			return nil
		}
		if p.Stage != "link" {
			return delivery.ErrInvalid
		}
		busy, err := q.RouteTransitionBusy(ctx, db.RouteTransitionBusyParams{AccountID: row.AccountID, WhatsappChat: p.ChatID, JobID: row.ID})
		if err != nil {
			return err
		}
		if busy {
			return delivery.ErrLocalRetry
		}
		route, err := transitionRoute(ctx, q, row, p)
		if err != nil {
			return err
		}
		other, err := q.OtherDestinationRoute(ctx, db.OtherDestinationRouteParams{AccountID: row.AccountID, TelegramChatID: p.FromChat, WhatsappChat: p.ChatID})
		if err != nil {
			return err
		}
		if other {
			return delivery.ErrInvalid
		}
		if route.TelegramChatID == p.Management && route.ThreadID > 1 {
			if err := q.RememberInbox(ctx, db.RememberInboxParams{AccountID: row.AccountID, WhatsappChat: p.ChatID, TelegramChatID: p.Management, ThreadID: route.ThreadID}); err != nil {
				return err
			}
		}
		if err := q.MoveMirrorRoute(ctx, db.MoveMirrorRouteParams{AccountID: row.AccountID, WhatsappChat: p.ChatID, TelegramChatID: p.FromChat, ThreadID: 0, Mode: "group"}); err != nil {
			return err
		}
		text := "Linked WhatsApp group " + p.ChatID + ". Your messages here will be sent to that group. Use /history 50 to import recent messages here, or /unmirror to move back to a management topic."
		if err := mirrorNotice(ctx, q, row, p, text, "", nil); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"chatId": p.ChatID})
		_, err = q.Enqueue(ctx, db.EnqueueParams{AccountID: row.AccountID, Dedup: "retire-inbox:" + row.Dedup, Stream: "retire-inbox:" + p.ChatID, Kind: "control-retire-inbox", Payload: payload})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		payload, _ = json.Marshal(map[string]any{"id": row.Dedup + ":metadata", "type": "sync_metadata", "data": map[string]string{"scope": "group", "id": p.ChatID, "source": "link", "requestId": row.Dedup}, "at": time.Now().UnixMilli()})
		if err := enqueueTransitionEvent(ctx, q, row.AccountID, row.Dedup+":metadata", payload); err != nil {
			return err
		}
		p.Stage = "linked"
		return saveTransition(ctx, q, row, p, false)
	})
}
func (s *Store) CommitInboxReturn(ctx context.Context, j delivery.Job, thread int64) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, err := loadTransition(ctx, q, row)
		if err != nil {
			return err
		}
		if p.Kind != "return_to_inbox" {
			return delivery.ErrInvalid
		}
		if p.Stage == "returned" {
			if p.ThreadID != thread {
				return delivery.ErrInvalid
			}
			return nil
		}
		if p.Stage != "create" || thread <= 1 {
			return delivery.ErrInvalid
		}
		effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: row.Dedup + ":topic"})
		if err != nil {
			return err
		}
		var topic models.ForumTopic
		if effect.State != "done" || json.Unmarshal(effect.Result, &topic) != nil || int64(topic.MessageThreadID) != thread {
			return delivery.ErrUncertain
		}
		route, err := transitionRoute(ctx, q, row, p)
		if err != nil {
			return err
		}
		reserved, err := q.ReservedTopic(ctx, db.ReservedTopicParams{AccountID: row.AccountID, TelegramChatID: p.Management, ThreadID: thread})
		if err != nil {
			return err
		}
		if reserved {
			return delivery.ErrInvalid
		}
		other, err := q.OtherTopicRoute(ctx, db.OtherTopicRouteParams{AccountID: row.AccountID, TelegramChatID: p.Management, ThreadID: thread, WhatsappChat: p.ChatID})
		if err != nil {
			return err
		}
		if other {
			return delivery.ErrInvalid
		}
		active, err := q.ActiveHistory(ctx, db.ActiveHistoryParams{AccountID: row.AccountID, WhatsappChat: p.ChatID})
		if err != nil {
			return err
		}
		if active {
			return delivery.ErrLocalRetry
		}
		mode, paused := "inbox", ""
		if route.Mode == "muted" || route.Mode == "ignored" {
			mode = route.Mode
			paused = " Use /keep in the topic to resume this conversation."
		}
		if err := q.MoveMirrorRoute(ctx, db.MoveMirrorRouteParams{AccountID: row.AccountID, WhatsappChat: p.ChatID, TelegramChatID: p.Management, ThreadID: thread, Mode: mode}); err != nil {
			return err
		}
		if err := q.SaveMirrorReturn(ctx, db.SaveMirrorReturnParams{AccountID: row.AccountID, WhatsappChat: p.ChatID, ReturnKey: row.Dedup}); err != nil {
			return err
		}
		if err := q.RememberInbox(ctx, db.RememberInboxParams{AccountID: row.AccountID, WhatsappChat: p.ChatID, TelegramChatID: p.Management, ThreadID: thread}); err != nil {
			return err
		}
		url := "https://t.me/c/" + strings.TrimPrefix(strconv.FormatInt(p.Management, 10), "-100") + "/" + strconv.FormatInt(thread, 10)
		markup := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: "Open management topic", URL: url}}}}
		if err := mirrorNotice(ctx, q, row, p, "Moved back to a management topic. This group’s existing messages stay here. Use /history 50 in the topic to import recent messages."+paused, ":confirmed", markup); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"id": row.Dedup + ":inbox", "type": "new_conversation", "data": map[string]any{"chatId": p.ChatID, "tgChat": p.Management, "thread": thread, "returnKey": row.Dedup}, "at": time.Now().UnixMilli()})
		if err := enqueueTransitionEvent(ctx, q, row.AccountID, row.Dedup+":inbox", payload); err != nil {
			return err
		}
		p.Stage = "returned"
		p.ThreadID = thread
		if err := saveTransition(ctx, q, row, p, false); err != nil {
			return err
		}
		return q.UnlockRouteTransition(ctx, db.UnlockRouteTransitionParams{AccountID: row.AccountID, WhatsappChat: p.ChatID, JobID: row.ID})
	})
}
func (s *Store) RefuseInboxReturn(ctx context.Context, j delivery.Job) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, err := loadTransition(ctx, q, row)
		if err != nil {
			return err
		}
		if p.Stage == "notice" {
			return nil
		}
		if p.Stage != "create" {
			return delivery.ErrInvalid
		}
		effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: row.Dedup + ":topic"})
		if err != nil {
			return err
		}
		if effect.State != "discarded" {
			return delivery.ErrInvalid
		}
		if err := mirrorNotice(ctx, q, row, p, control.UnmirrorPermission, ":permission", nil); err != nil {
			return err
		}
		p.Stage = "notice"
		if err := saveTransition(ctx, q, row, p, false); err != nil {
			return err
		}
		return q.UnlockRouteTransition(ctx, db.UnlockRouteTransitionParams{AccountID: row.AccountID, WhatsappChat: p.ChatID, JobID: row.ID})
	})
}
func (s *Store) MirrorNoticeCurrent(ctx context.Context, j delivery.Job) (bool, error) {
	current := true
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		p, err := loadTransition(ctx, q, row)
		if err != nil {
			return err
		}
		if p.Stage != "returned" {
			return nil
		}
		r, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: p.ChatID})
		if err != nil {
			return err
		}
		key, err := q.MirrorReturnKey(ctx, db.MirrorReturnKeyParams{AccountID: row.AccountID, WhatsappChat: p.ChatID})
		if err != nil {
			return err
		}
		current = r.TelegramChatID == p.Management && r.ThreadID == p.ThreadID && key == row.Dedup
		return nil
	})
	return current, err
}
func (s *Store) CancelMirrorNotice(ctx context.Context, j delivery.Job) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		p, err := loadTransition(ctx, q, row)
		if err != nil {
			return err
		}
		if p.Stage != "returned" {
			return delivery.ErrInvalid
		}
		r, err := q.ControlPlan(ctx, db.ControlPlanParams{AccountID: row.AccountID, PlanKey: row.Dedup})
		if err != nil {
			return err
		}
		plan, err := decodeControl(r)
		if err != nil {
			return err
		}
		for i := range plan.Packets {
			effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: plan.PacketKey(i)})
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			switch effect.State {
			case "done":
				if err := recordControl(ctx, q, row.AccountID, plan, i); err != nil {
					return err
				}
			case "pending":
				if _, err := q.DiscardEffect(ctx, db.DiscardEffectParams{AccountID: row.AccountID, EffectKey: effect.EffectKey, JobID: row.ID}); err != nil {
					return err
				}
			case "discarded":
			default:
				return delivery.ErrUncertain
			}
		}
		return q.CompleteControl(ctx, db.CompleteControlParams{AccountID: row.AccountID, PlanKey: row.Dedup})
	})
}

// RecordHistoryActivity coordinates a future import's active phase with route
// moves. It does not fetch history or replace its durable plans/receipts.
func (s *Store) RecordHistoryActivity(ctx context.Context, j delivery.Job, chat, run, status string) error {
	if chat == "" || run == "" || (status != "queued" && status != "fetching" && status != "delivering" && status != "complete" && status != "failed" && status != "cancelled") {
		return delivery.ErrInvalid
	}
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "management" && row.Kind != "history" && row.Kind != "history-message" && row.Kind != "history-finish" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if status == "queued" || status == "fetching" || status == "delivering" {
			busy, err := q.RouteTransitionBusy(ctx, db.RouteTransitionBusyParams{AccountID: row.AccountID, WhatsappChat: chat, JobID: row.ID})
			if err != nil {
				return err
			}
			if busy {
				return delivery.ErrLocalRetry
			}
		}
		n, err := q.RecordHistoryActivity(ctx, db.RecordHistoryActivityParams{AccountID: row.AccountID, WhatsappChat: chat, RunID: run, Status: status})
		if err != nil {
			return err
		}
		if n != 1 {
			return delivery.ErrInvalid
		}
		return nil
	})
}

var _ control.MirrorStore = (*Store)(nil)
