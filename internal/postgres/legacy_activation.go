package postgres

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

// LegacyRuntimeConfiguration returns the retained configuration for startup
// identity checks. It never installs archived environment values.
func (s *Store) LegacyRuntimeConfiguration(ctx context.Context, account string, key [32]byte) (legacy.Configuration, bool, error) {
	checkpoint, err := s.queries.RuntimeLegacyImport(ctx, account)
	if errors.Is(err, pgx.ErrNoRows) {
		return legacy.Configuration{}, false, nil
	}
	if err != nil {
		return legacy.Configuration{}, false, err
	}
	c, err := legacyConfiguration(checkpoint, key)
	return c, err == nil, err
}

// ActivateLegacyImport releases only the import gate. The explicit runtime
// must still own the account, verify native providers and complete bot setup.
// Call after a stopped-writer asset comparison, before starting new writers.
func (s *Store) ActivateLegacyImport(ctx context.Context, bundle *legacy.Bundle, receipt legacy.AssetVerification) error {
	if bundle == nil || receipt.Account != bundle.Account || receipt.ArchiveDigest != bundle.Digest {
		return legacy.ErrInvalid
	}
	for _, tree := range []legacy.AssetTree{receipt.Media, receipt.Telegram} {
		hash, err := hex.DecodeString(tree.Digest)
		if err != nil || len(hash) != 32 || tree.Files < 0 || tree.Bytes < 0 {
			return legacy.ErrInvalid
		}
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
	if identity.AccountID != bundle.Account {
		return ErrLegacyDestination
	}
	checkpoint, err := q.LockLegacyImport(ctx, bundle.Account)
	if err != nil {
		return err
	}
	if checkpoint.ArchiveDigest != bundle.Digest {
		return ErrLegacyDestination
	}
	saved, err := legacyConfiguration(checkpoint, bundle.Configuration().EncryptionKey())
	if err != nil {
		return err
	}
	a, b := saved.Files()
	c, d := bundle.Configuration().Files()
	if !bytes.Equal(a, c) || !bytes.Equal(b, d) {
		return ErrLegacyDestination
	}
	policy, err := q.LegacyCutoverPolicy(ctx, bundle.Account)
	if err != nil || policy.ArchiveDigest != bundle.Digest || policy.QueueMode != "fresh" {
		return ErrLegacyPending
	}
	complete, err := q.LegacyProjectionComplete(ctx, bundle.Account)
	if err != nil {
		return err
	}
	if !complete.Bool || !complete.Valid {
		return ErrLegacyPending
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return legacy.ErrInvalid
	}
	if checkpoint.ActivatedAt.Valid {
		previous, err := q.LegacyAssetVerification(ctx, bundle.Account)
		if err != nil {
			return err
		}
		var old legacy.AssetVerification
		if previous.ArchiveDigest != bundle.Digest || json.Unmarshal(previous.Receipt, &old) != nil || old != receipt {
			return ErrLegacyDestination
		}
		return nil
	}
	// No admission may race the fresh-queue decision. Never discard native work.
	if _, err = tx.Exec(ctx, "LOCK TABLE jobs, effects IN SHARE ROW EXCLUSIVE MODE NOWAIT"); err != nil {
		return err
	}
	empty, err := q.LegacyNativeQueueEmpty(ctx, bundle.Account)
	if err != nil {
		return err
	}
	if !empty.Bool || !empty.Valid {
		return ErrLegacyDestination
	}
	if err = q.SaveLegacyAssetVerification(ctx, db.SaveLegacyAssetVerificationParams{AccountID: bundle.Account, ArchiveDigest: bundle.Digest, Receipt: raw}); err != nil {
		return err
	}
	n, err := q.ActivateLegacyImport(ctx, db.ActivateLegacyImportParams{AccountID: bundle.Account, ArchiveDigest: bundle.Digest})
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLegacyDestination
	}
	return tx.Commit(ctx)
}
