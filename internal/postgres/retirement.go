package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func loadRetirement(ctx context.Context, q *db.Queries, row db.Job) (conversation.Retirement, error) {
	r, err := q.InboxRetirement(ctx, db.InboxRetirementParams{AccountID: row.AccountID, JobID: row.ID})
	if err != nil {
		return conversation.Retirement{}, err
	}
	var p conversation.Retirement
	if json.Unmarshal(r.Payload, &p) != nil || p.Management >= 0 || p.ChatID == "" {
		return p, delivery.ErrInvalid
	}
	p.Complete = r.Complete
	for i, topic := range p.Topics {
		t, err := q.RetiredTopic(ctx, db.RetiredTopicParams{AccountID: row.AccountID, TelegramChatID: p.Management, ThreadID: topic.ThreadID})
		if err != nil {
			return p, err
		}
		if t.WhatsappChat != p.ChatID || topic.ThreadID <= 1 {
			return p, delivery.ErrInvalid
		}
		p.Topics[i].Deleted = t.Deleted
	}
	return p, nil
}

func (s *Store) PrepareInboxRetirement(ctx context.Context, job delivery.Job, management int64) (conversation.Retirement, error) {
	if management >= 0 {
		return conversation.Retirement{}, delivery.ErrInvalid
	}
	var result conversation.Retirement
	err := s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		var source struct {
			ChatID string `json:"chatId"`
		}
		if row.Kind != "control-retire-inbox" || json.Unmarshal(row.Payload, &source) != nil || source.ChatID == "" || row.Stream != "retire-inbox:"+source.ChatID {
			return delivery.ErrInvalid
		}
		// Route creation, configuration, relay-plan admission and retirement
		// share this short transaction lock. No lock spans the provider call.
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, err := loadRetirement(ctx, q, row)
		if err == nil {
			result = p
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		p = conversation.Retirement{ChatID: source.ChatID, Management: management, CurrentThread: -1, Complete: true}
		route, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: source.ChatID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		eligible := err == nil && route.Mode == "group" && route.TelegramChatID != management
		if err == nil && route.Mode == "dedicated" {
			d, err := directory(ctx, q, row.AccountID)
			if err != nil {
				return err
			}
			person := d.Person(source.ChatID, nil)
			eligible = person.Registered && person.Bot.Started
		}
		if eligible {
			if route.TelegramChatID == management {
				p.CurrentThread = route.ThreadID
			}
			topics, err := q.RetirementTopics(ctx, db.RetirementTopicsParams{AccountID: row.AccountID, WhatsappChat: source.ChatID, Management: management})
			if err != nil {
				return err
			}
			for _, thread := range topics {
				shared, err := q.SharedRetirementTopic(ctx, db.SharedRetirementTopicParams{AccountID: row.AccountID, WhatsappChat: source.ChatID, Management: management, ThreadID: thread})
				if err != nil {
					return err
				}
				if shared {
					return conversation.ErrSharedInbox
				}
				pending, err := q.IncompleteTopicRelay(ctx, db.IncompleteTopicRelayParams{AccountID: row.AccountID, Column2: management, Column3: thread})
				if err != nil {
					return err
				}
				if pending {
					return delivery.ErrLocalRetry
				}
				if err := q.ReserveRetirementTopic(ctx, db.ReserveRetirementTopicParams{AccountID: row.AccountID, TelegramChatID: management, ThreadID: thread, WhatsappChat: source.ChatID}); err != nil {
					return err
				}
				p.Topics = append(p.Topics, conversation.RetiringTopic{ThreadID: thread})
			}
			p.Complete = false
		}
		payload, _ := json.Marshal(p)
		if err := q.PrepareInboxRetirement(ctx, db.PrepareInboxRetirementParams{AccountID: row.AccountID, JobID: row.ID, Payload: payload, Complete: p.Complete}); err != nil {
			return err
		}
		result, err = loadRetirement(ctx, q, row)
		return err
	})
	return result, err
}

func (s *Store) FinishTopicRetirement(ctx context.Context, job delivery.Job, thread int64) error {
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if row.Kind != "control-retire-inbox" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, err := loadRetirement(ctx, q, row)
		if err != nil {
			return err
		}
		for _, topic := range p.Topics {
			if topic.ThreadID == thread {
				if err := q.DeleteRetirementTopic(ctx, db.DeleteRetirementTopicParams{AccountID: row.AccountID, TelegramChatID: p.Management, ThreadID: thread}); err != nil {
					return err
				}
				return q.MarkRetiredTopicObservations(ctx, db.MarkRetiredTopicObservationsParams{AccountID: row.AccountID, ChatID: p.Management, ThreadID: thread})
			}
		}
		return delivery.ErrInvalid
	})
}

func (s *Store) CompleteInboxRetirement(ctx context.Context, job delivery.Job) error {
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if row.Kind != "control-retire-inbox" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, err := loadRetirement(ctx, q, row)
		if err != nil || p.Complete {
			return err
		}
		threads := []int64{}
		for _, topic := range p.Topics {
			if !topic.Deleted {
				return delivery.ErrIncomplete
			}
			threads = append(threads, topic.ThreadID)
		}
		if p.CurrentThread >= 0 {
			threads = append(threads, p.CurrentThread)
		}
		if err := q.RetireCurrentInbox(ctx, db.RetireCurrentInboxParams{AccountID: row.AccountID, WhatsappChat: p.ChatID, TelegramChatID: p.Management, Column4: threads}); err != nil {
			return err
		}
		return q.CompleteInboxRetirement(ctx, db.CompleteInboxRetirementParams{AccountID: row.AccountID, JobID: row.ID})
	})
}

var _ conversation.RetirementStore = (*Store)(nil)
