package postgres

import (
	"context"
	"encoding/json"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

// NormalizeChange aligns mapped source changes with the same canonical stream
// as ordinary messages before creating any remote effect.
func (s *Store) NormalizeChange(ctx context.Context, j delivery.Job, session string) error {
	deferred := false
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "wa" || session == "" {
			return delivery.ErrInvalid
		}
		if err := q.LockQueueTopology(ctx); err != nil {
			return err
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		var hook struct {
			Session string `json:"sessionId"`
			Account string `json:"accountId"`
			Key     string `json:"idempotencyKey"`
			Data    struct {
				ChatID string `json:"chatId"`
			} `json:"data"`
		}
		if json.Unmarshal(row.Payload, &hook) != nil || hook.Session != session || hook.Account != row.AccountID || hook.Key == "" || row.Dedup != "ow:"+hook.Key || hook.Data.ChatID == "" {
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
		stream := "wa:" + chat
		if row.Stream == stream {
			return nil
		}
		count, err := q.JobEffectCount(ctx, row.ID)
		if err != nil {
			return err
		}
		if count != 0 {
			return delivery.ErrInvalid
		}
		busy, err := q.ConversationBusy(ctx, db.ConversationBusyParams{AccountID: row.AccountID, Stream: stream, JobID: row.ID})
		if err != nil {
			return err
		}
		if busy {
			deferred = true
			return q.ParkRestreamedJob(ctx, db.ParkRestreamedJobParams{ID: row.ID, Stream: stream})
		}
		return q.RestreamJob(ctx, db.RestreamJobParams{ID: row.ID, Stream: stream})
	})
	if err != nil {
		return err
	}
	if deferred {
		return delivery.ErrDeferred
	}
	return nil
}
