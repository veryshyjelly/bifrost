package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

// ChooseLegacyFreshQueue records the owner's explicit choice to leave original
// jobs/history runs in immutable evidence and start the new queue empty.
// It never marks source jobs/effects complete, replays sends or activates work.
func (s *Store) ChooseLegacyFreshQueue(ctx context.Context, account, digest string) error {
	if account == "" || digest == "" {
		return legacy.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	q := s.queries.WithTx(tx)
	if err = q.LockIdentity(ctx); err != nil {
		return err
	}
	identity, err := q.Identity(ctx)
	if err != nil {
		return err
	}
	if identity.AccountID != account {
		return ErrLegacyDestination
	}
	checkpoint, err := q.LockLegacyImport(ctx, account)
	if err != nil {
		return err
	}
	if checkpoint.ArchiveDigest != digest {
		return ErrLegacyDestination
	}
	if previous, err := q.LegacyCutoverPolicy(ctx, account); err == nil {
		if previous.ArchiveDigest != digest || previous.QueueMode != "fresh" {
			return legacy.ErrInvalid
		}
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if checkpoint.ActivatedAt.Valid {
		return legacy.ErrInvalid
	}
	if _, err = q.LegacyStatusProjection(ctx, account); err != nil {
		return err
	}
	if err = q.SaveLegacyCutoverPolicy(ctx, db.SaveLegacyCutoverPolicyParams{AccountID: account, ArchiveDigest: digest}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
