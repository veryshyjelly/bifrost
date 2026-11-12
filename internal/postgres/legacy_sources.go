package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

type LegacySourceProjection struct {
	Account       string `json:"account"`
	ArchiveDigest string `json:"archive_digest"`
	Sources       int    `json:"sources"`
}

// ProjectLegacySources retains only the original identity/context subset,
// independently of provider receipts and job-linked content snapshots.
func (s *Store) ProjectLegacySources(ctx context.Context, account string) (LegacySourceProjection, error) {
	var result LegacySourceProjection
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
	previous, err := q.LegacySourceProjection(ctx, account)
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
	if _, err = q.LegacyMessageProjection(ctx, account); err != nil {
		return result, err
	}
	if err = q.LockContactDirectory(ctx, account); err != nil {
		return result, err
	}
	conflict, err := q.RetainedSourceConflict(ctx, account)
	if err != nil {
		return result, err
	}
	if conflict {
		return result, ErrLegacyDestination
	}
	directory, err := directory(ctx, q, account)
	if err != nil {
		return result, err
	}
	values, err := q.LegacySourceValues(ctx, account)
	if err != nil {
		return result, err
	}
	input := make([]legacy.KV, len(values))
	for i, v := range values {
		input[i] = legacy.KV{Kind: v.Kind, ID: v.SourceID, Value: v.SourceValue}
	}
	sources, err := legacy.ProjectSources(input, directory)
	if err != nil {
		return result, err
	}
	for _, source := range sources {
		raw, _ := json.Marshal(source)
		if err = q.SaveRetainedMessageSource(ctx, db.SaveRetainedMessageSourceParams{AccountID: account, WhatsappChat: source.ChatID, MessageID: source.MessageID, SourceKey: source.SourceKey, Payload: raw}); err != nil {
			return LegacySourceProjection{}, err
		}
	}
	result = LegacySourceProjection{Account: account, ArchiveDigest: checkpoint.ArchiveDigest, Sources: len(sources)}
	raw, _ := json.Marshal(result)
	if err = q.SaveLegacySourceProjection(ctx, db.SaveLegacySourceProjectionParams{AccountID: account, Result: raw}); err != nil {
		return LegacySourceProjection{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return LegacySourceProjection{}, err
	}
	return result, nil
}
