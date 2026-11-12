package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func (s *Store) MessageComplete(ctx context.Context, account, chat, id string) (bool, error) {
	return s.queries.MessageComplete(ctx, db.MessageCompleteParams{AccountID: account, WhatsappChat: chat, MessageID: id})
}

type LegacyCompletionProjection struct {
	Account       string `json:"account"`
	ArchiveDigest string `json:"archive_digest"`
	Messages      int    `json:"messages"`
}

// ProjectLegacyCompletions imports explicit completed live-delivery evidence.
// It does not adopt effects, mark pending source work complete or activate an
// account. Existing native frozen plans retain precedence over this guard.
func (s *Store) ProjectLegacyCompletions(ctx context.Context, account string) (LegacyCompletionProjection, error) {
	var result LegacyCompletionProjection
	if account == "" {
		return result, legacy.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer rollback(tx)
	q := s.queries.WithTx(tx)
	if err = q.LockIdentity(ctx); err != nil {
		return result, err
	}
	saved, err := q.Identity(ctx)
	if err != nil {
		return result, err
	}
	if saved.AccountID != account {
		return result, ErrLegacyDestination
	}
	checkpoint, err := q.LockLegacyImport(ctx, account)
	if err != nil {
		return result, err
	}
	previous, err := q.LegacyCompletionProjection(ctx, account)
	if err == nil {
		if json.Unmarshal(previous, &result) != nil || result.Account != account || result.ArchiveDigest != checkpoint.ArchiveDigest {
			return result, legacy.ErrInvalid
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if checkpoint.ActivatedAt.Valid {
		return result, legacy.ErrInvalid
	}
	if _, err = q.LegacyRouteProjection(ctx, account); err != nil {
		return result, err
	}
	if err = q.LockContactDirectory(ctx, account); err != nil {
		return result, err
	}
	conflict, err := q.LegacyCompletionConflict(ctx, account)
	if err != nil {
		return result, err
	}
	if conflict {
		return result, ErrLegacyDestination
	}
	d, err := directory(ctx, q, account)
	if err != nil {
		return result, err
	}
	values, err := q.LegacyCompletionValues(ctx, account)
	if err != nil {
		return result, err
	}
	input := make([]legacy.KV, len(values))
	for i, v := range values {
		input[i] = legacy.KV{Kind: v.Kind, ID: v.SourceID, Value: v.SourceValue}
	}
	completed, err := legacy.ProjectCompletions(input, d)
	if err != nil {
		return result, err
	}
	for _, m := range completed {
		if err = q.RecordSourceCompletion(ctx, db.RecordSourceCompletionParams{AccountID: account, WhatsappChat: m.ChatID, MessageID: m.MessageID, SourceKey: m.SourceKey}); err != nil {
			return LegacyCompletionProjection{}, err
		}
	}
	result = LegacyCompletionProjection{Account: account, ArchiveDigest: checkpoint.ArchiveDigest, Messages: len(completed)}
	raw, _ := json.Marshal(result)
	if err = q.SaveLegacyCompletionProjection(ctx, db.SaveLegacyCompletionProjectionParams{AccountID: account, Result: raw}); err != nil {
		return LegacyCompletionProjection{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return LegacyCompletionProjection{}, err
	}
	return result, nil
}
