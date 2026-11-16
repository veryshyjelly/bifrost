package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/recovery"
)

// ReconcileTopic journals an explicit topic decision under topology and job
// locks. Identical operation replays reuse their saved result; changed inspection
// evidence is refused before modifying reservations or recovery state.
func (s *Store) ReconcileTopic(ctx context.Context, d recovery.TopicDecision) (recovery.TopicResult, error) {
	var result recovery.TopicResult
	if !d.Valid() || int64(int(d.ThreadID)) != d.ThreadID {
		return result, delivery.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer rollback(tx)
	q := s.queries.WithTx(tx)
	if err := q.LockQueueTopology(ctx); err != nil {
		return result, err
	}
	identity, err := q.Identity(ctx)
	if err != nil {
		return result, err
	}
	if identity.AccountID != d.AccountID {
		return result, delivery.ErrInvalid
	}
	if old, err := q.TopicReconciliation(ctx, db.TopicReconciliationParams{AccountID: d.AccountID, OperationID: d.OperationID}); err == nil {
		var previous recovery.TopicDecision
		if json.Unmarshal(old.Request, &previous) != nil || !reflect.DeepEqual(previous, d) || json.Unmarshal(old.Result, &result) != nil || result.Job != d.Job || result.Effect != d.Effect {
			return result, delivery.ErrInvalid
		}
		return result, tx.Commit(ctx)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	row, err := q.LockTopicReviewJob(ctx, d.Job)
	var locked *pgconn.PgError
	if errors.As(err, &locked) && locked.Code == "55P03" {
		return result, recovery.ErrConflict
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return result, delivery.ErrInvalid
	}
	if err != nil {
		return result, err
	}
	if row.AccountID != d.AccountID {
		return result, delivery.ErrInvalid
	}
	if row.State != "failed" && row.State != "uncertain" && row.State != "done" {
		return result, recovery.ErrConflict
	}
	if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
		return result, err
	}
	snapshot, err := inspectTopic(ctx, q, row)
	if err != nil {
		return result, err
	}
	if snapshot.Blocked != "" || snapshot.State != d.ExpectedState || snapshot.EffectState != d.ExpectedEffectState || snapshot.RequestDigest != d.RequestDigest || snapshot.Effect != d.Effect || snapshot.Management != d.Management {
		return result, recovery.ErrConflict
	}
	scope, err := topicScopeFor(ctx, q, row)
	if err != nil {
		return result, err
	}
	effect, err := topicReviewEffect(ctx, q, row, scope)
	if err != nil {
		return result, err
	}
	if d.Action == "adopt" {
		if scope.native != nil && scope.native.Route.TelegramChatID == d.Management && scope.native.Route.ThreadID > 1 && scope.native.Route.ThreadID == d.ThreadID {
			return result, recovery.ErrConflict
		}
		reserved, err := q.ReservedTopic(ctx, db.ReservedTopicParams{AccountID: row.AccountID, TelegramChatID: d.Management, ThreadID: d.ThreadID})
		if err != nil {
			return result, err
		}
		if reserved {
			return result, recovery.ErrConflict
		}
		other, err := q.OtherTopicRoute(ctx, db.OtherTopicRouteParams{AccountID: row.AccountID, TelegramChatID: d.Management, ThreadID: d.ThreadID, WhatsappChat: scope.chat})
		if err != nil {
			return result, err
		}
		if other {
			return result, recovery.ErrConflict
		}
	}
	if effect.State == "unprepared" {
		if err := q.CreateEffect(ctx, db.CreateEffectParams{AccountID: row.AccountID, EffectKey: scope.key, JobID: row.ID, Request: effect.Request}); err != nil {
			return result, err
		}
		effect.State = "pending"
	}
	switch d.Action {
	case "adopt":
		if effect.State == "done" {
			var topic models.ForumTopic
			if json.Unmarshal(effect.Result, &topic) != nil || int64(topic.MessageThreadID) != d.ThreadID {
				return result, recovery.ErrConflict
			}
		} else {
			if effect.State != "pending" && effect.State != "uncertain" {
				return result, recovery.ErrConflict
			}
			receipt, _ := json.Marshal(models.ForumTopic{MessageThreadID: int(d.ThreadID), Name: snapshot.Name})
			n, err := q.AdoptReviewedTopic(ctx, db.AdoptReviewedTopicParams{AccountID: row.AccountID, JobID: row.ID, EffectKey: scope.key, Result: receipt})
			if err != nil {
				return result, err
			}
			if n != 1 {
				return result, recovery.ErrConflict
			}
		}
	case "absent":
		if effect.State != "pending" && effect.State != "uncertain" {
			return result, recovery.ErrConflict
		}
		n, err := q.ResetReviewedTopic(ctx, db.ResetReviewedTopicParams{AccountID: row.AccountID, JobID: row.ID, EffectKey: scope.key})
		if err != nil {
			return result, err
		}
		if n != 1 {
			return result, recovery.ErrConflict
		}
	default:
		return result, delivery.ErrInvalid
	}
	if err := q.SetJobState(ctx, db.SetJobStateParams{ID: row.ID, State: "pending"}); err != nil {
		return result, err
	}
	result = recovery.TopicResult{Job: row.ID, Effect: scope.key, Action: d.Action, ThreadID: d.ThreadID, State: "pending"}
	request, _ := json.Marshal(d)
	rawResult, _ := json.Marshal(result)
	if err := q.CreateTopicReconciliation(ctx, db.CreateTopicReconciliationParams{AccountID: row.AccountID, OperationID: d.OperationID, JobID: row.ID, EffectKey: scope.key, Request: request, Result: rawResult}); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

var _ recovery.Topics = (*Store)(nil)
