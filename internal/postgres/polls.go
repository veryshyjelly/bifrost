package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/polls"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

func validPollSettings(c polls.Settings) bool {
	return c.Session != "" && c.OwnerID > 0 && c.NoticeTTL >= 5 && c.NoticeTTL <= 86400
}

func (s *Store) PreparePollCallback(ctx context.Context, j delivery.Job, c polls.Settings) (control.Plan, error) {
	var result control.Plan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "tg" || !validPollSettings(c) {
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
		if json.Unmarshal(row.Payload, &envelope) != nil || json.Unmarshal(envelope.Update, &u) != nil || envelope.Bot == "" || u.CallbackQuery == nil || row.Dedup != "tg:"+envelope.Bot+":"+strconv.FormatInt(u.ID, 10) || !ingress.AuthorizedUpdate(envelope.Update, &u, c.OwnerID) {
			return delivery.ErrInvalid
		}
		callback := u.CallbackQuery
		if callback.From.IsBot || callback.From.ID != c.OwnerID || !strings.HasPrefix(callback.Data, "vote:") {
			return delivery.ErrInvalid
		}
		p := control.Plan{Key: row.Dedup, Target: control.Target{BotID: envelope.Bot}, Answer: &bot.AnswerCallbackQueryParams{CallbackQueryID: callback.ID, Text: "This poll choice is no longer active."}, ReplyTTLSeconds: c.NoticeTTL}
		save := func() error {
			raw, err := json.Marshal(p)
			if err != nil {
				return err
			}
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
		id := strings.TrimPrefix(callback.Data, "vote:")
		actionRow, err := q.NativePollAction(ctx, db.NativePollActionParams{AccountID: row.AccountID, ActionID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return save()
		}
		if err != nil {
			return err
		}
		var action relay.PollAction
		if json.Unmarshal(actionRow.Payload, &action) != nil || action.ID != id {
			return delivery.ErrInvalid
		}
		if action.Session != c.Session || action.OwnerID != c.OwnerID || action.ExpiresAt <= time.Now().UnixMilli() || action.Target != p.Target {
			return save()
		}
		mapped, err := q.TelegramMapping(ctx, db.TelegramMappingParams{AccountID: row.AccountID, Scope: relayScope(envelope.Bot, m.Chat.ID), TelegramChatID: m.Chat.ID, TelegramMessageID: int64(m.ID)})
		if errors.Is(err, pgx.ErrNoRows) {
			return save()
		}
		if err != nil {
			return err
		}
		if mapped.PlanKey.String != actionRow.PlanKey || mapped.Deleted || mapped.ObservationDeleted || mapped.WhatsappChat != action.ChatID || mapped.WhatsappMessageID != action.MessageID || mapped.ThreadID != action.Target.ThreadID {
			return save()
		}
		revoked, err := q.HistoryMessageRevoked(ctx, db.HistoryMessageRevokedParams{AccountID: row.AccountID, WhatsappChat: action.ChatID, MessageID: action.MessageID})
		if err != nil {
			return err
		}
		if revoked {
			return save()
		}
		if m.Chat.ID > 0 {
			contact, err := q.ContactForBot(ctx, db.ContactForBotParams{AccountID: row.AccountID, BotID: envelope.Bot})
			if errors.Is(err, pgx.ErrNoRows) {
				return save()
			}
			if err != nil {
				return err
			}
			d, err := directory(ctx, q, row.AccountID)
			if err != nil {
				return err
			}
			chat, err := conversationKey(ctx, q, row.AccountID, contact, d)
			if err != nil {
				return err
			}
			if chat != action.ChatID {
				return save()
			}
		} else {
			route, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: action.ChatID})
			if err != nil {
				return err
			}
			if route.TelegramChatID != m.Chat.ID || route.ThreadID != int64(m.MessageThreadID) {
				return save()
			}
		}
		payload, err := json.Marshal(action)
		if err != nil {
			return err
		}
		job, err := metadataEnqueue(ctx, q, row.AccountID, row.Dedup+":poll-vote", "poll:"+action.ChatID+":"+action.MessageID, "control-poll-vote", action)
		if err != nil {
			return err
		}
		if err := q.CreatePollVoteJob(ctx, db.CreatePollVoteJobParams{AccountID: row.AccountID, JobID: job, SourceJobID: row.ID, Payload: payload}); err != nil {
			return err
		}
		p.Answer.Text = ""
		return save()
	})
	return result, err
}
func pollVote(ctx context.Context, q *db.Queries, row db.Job, c polls.Settings) (db.PollVoteJob, polls.Vote, error) {
	stored, err := q.PollVoteJob(ctx, db.PollVoteJobParams{AccountID: row.AccountID, JobID: row.ID})
	if err != nil {
		return stored, polls.Vote{}, err
	}
	var p polls.Vote
	if len(stored.Plan) > 0 && string(stored.Plan) != "null" {
		if json.Unmarshal(stored.Plan, &p) != nil || p.Key != row.Dedup || p.Session != c.Session {
			return stored, p, delivery.ErrInvalid
		}
		return stored, p, nil
	}
	var action, payload relay.PollAction
	if row.Kind != "control-poll-vote" || json.Unmarshal(stored.Payload, &action) != nil || json.Unmarshal(row.Payload, &payload) != nil || !reflect.DeepEqual(action, payload) || action.Session != c.Session || action.OwnerID != c.OwnerID {
		return stored, p, delivery.ErrInvalid
	}
	p = polls.Vote{Key: row.Dedup, Session: c.Session, State: "pending", NoticeTTL: c.NoticeTTL, Action: action, Request: sdk.VotePollRequest{ChatID: action.ChatID, PollMessageID: action.MessageID, Options: []string{action.Choice}}}
	if action.Multiple {
		previous, err := q.PollSelection(ctx, db.PollSelectionParams{AccountID: row.AccountID, WhatsappChat: action.ChatID, MessageID: action.MessageID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return stored, p, err
		}
		var selected []string
		if len(previous) > 0 && json.Unmarshal(previous, &selected) != nil {
			return stored, p, delivery.ErrInvalid
		}
		options := []string{}
		found := false
		for _, choice := range selected {
			if choice == action.Choice {
				found = true
			} else {
				options = append(options, choice)
			}
		}
		if !found {
			options = append(options, action.Choice)
		}
		p.Request.Options = options
	}
	return stored, p, nil
}
func savePollVote(ctx context.Context, q *db.Queries, row db.Job, p polls.Vote) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return q.SavePollVote(ctx, db.SavePollVoteParams{AccountID: row.AccountID, JobID: row.ID, Plan: raw})
}
func (s *Store) PreparePollVote(ctx context.Context, j delivery.Job, c polls.Settings) (polls.Vote, error) {
	var result polls.Vote
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "control-poll-vote" || !validPollSettings(c) {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		_, p, err := pollVote(ctx, q, row, c)
		if err != nil {
			return err
		}
		if row.Stream != "poll:"+p.Action.ChatID+":"+p.Action.MessageID {
			return delivery.ErrInvalid
		}
		if err := savePollVote(ctx, q, row, p); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}
func frozenPollVote(ctx context.Context, q *db.Queries, row db.Job) (polls.Vote, error) {
	stored, err := q.PollVoteJob(ctx, db.PollVoteJobParams{AccountID: row.AccountID, JobID: row.ID})
	if err != nil {
		return polls.Vote{}, err
	}
	var p polls.Vote
	if json.Unmarshal(stored.Plan, &p) != nil || p.Key != row.Dedup || row.Kind != "control-poll-vote" || row.Stream != "poll:"+p.Action.ChatID+":"+p.Action.MessageID {
		return p, delivery.ErrInvalid
	}
	if _, _, err := p.Frozen(); err != nil {
		return p, err
	}
	return p, nil
}
func (s *Store) CompletePollVote(ctx context.Context, j delivery.Job) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, err := frozenPollVote(ctx, q, row)
		if err != nil {
			return err
		}
		if p.State != "pending" && p.State != "done" {
			return delivery.ErrInvalid
		}
		options, _ := json.Marshal(p.Request.Options)
		if err := q.SetPollSelection(ctx, db.SetPollSelectionParams{AccountID: row.AccountID, WhatsappChat: p.Request.ChatID, MessageID: p.Request.PollMessageID, Options: options}); err != nil {
			return err
		}
		p.State = "done"
		return savePollVote(ctx, q, row, p)
	})
}
func (s *Store) RefusePollVote(ctx context.Context, j delivery.Job, status int) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		switch status {
		case 400, 403, 404, 501:
		default:
			return delivery.ErrInvalid
		}
		p, err := frozenPollVote(ctx, q, row)
		if err != nil {
			return err
		}
		if p.State == "refused" {
			return nil
		}
		if p.State != "pending" {
			return delivery.ErrInvalid
		}
		text := "This vote was not applied on WhatsApp. The poll may be unavailable or the provider may not support voting."
		if status == 501 {
			text = "The current WhatsApp engine does not support sending poll votes. Your previous selection was kept."
		}
		n := control.Notice{Target: p.Action.Target, Body: presentation.Plain(text), TTLSeconds: p.NoticeTTL}
		if _, err := metadataEnqueue(ctx, q, row.AccountID, row.Dedup+":notice", "notice:"+row.Dedup, "control-notice", n); err != nil {
			return err
		}
		p.State = "refused"
		return savePollVote(ctx, q, row, p)
	})
}
func (s *Store) ObservePollVote(ctx context.Context, j delivery.Job, c polls.Settings) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "wa" || !validPollSettings(c) {
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
				ChatID          string   `json:"chatId"`
				PollMessageID   string   `json:"pollMessageId"`
				MessageID       string   `json:"messageId"`
				ID              string   `json:"id"`
				VoterID         string   `json:"voterId"`
				SelectedOptions []string `json:"selectedOptions"`
			} `json:"data"`
		}
		if json.Unmarshal(row.Payload, &hook) != nil || hook.Event != sdk.EventPollVote || hook.Account != row.AccountID || hook.Session != c.Session || row.Dedup != "ow:"+hook.Key {
			return delivery.ErrInvalid
		}
		id := hook.Data.PollMessageID
		if id == "" {
			id = hook.Data.MessageID
		}
		if id == "" {
			id = hook.Data.ID
		}
		if id == "" || hook.Data.ChatID == "" {
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
		revoked, err := q.HistoryMessageRevoked(ctx, db.HistoryMessageRevokedParams{AccountID: row.AccountID, WhatsappChat: chat, MessageID: id})
		if err != nil {
			return err
		}
		if revoked {
			return nil
		}
		copies, err := q.RetainedSourceMappings(ctx, db.RetainedSourceMappingsParams{AccountID: row.AccountID, WhatsappChat: chat, WhatsappMessageID: id})
		if err != nil {
			return err
		}
		selection := strings.Join(hook.Data.SelectedOptions, ", ")
		if selection == "" {
			selection = "cleared"
		}
		for _, copy := range copies {
			target := control.Target{BotID: copy.BotID, ChatID: copy.TelegramChatID, ThreadID: copy.ThreadID}
			if target.ChatID < 0 {
				target.BotID = "master"
			}
			notice := control.Notice{Target: target, Body: presentation.Plain("[Poll vote · " + hook.Data.VoterID + "] " + selection), ReplyTo: copy.TelegramMessageID, TTLSeconds: c.NoticeTTL}
			if _, err := metadataEnqueue(ctx, q, row.AccountID, row.Dedup+":poll-notice:"+strconv.FormatInt(copy.Seq, 10), "notice:"+row.Dedup, "control-notice", notice); err != nil {
				return err
			}
		}
		return nil
	})
}

var _ polls.Store = (*Store)(nil)
