package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/secrets"
)

var ErrLegacyDestination = errors.New("legacy preparation requires an empty destination with the original identity and key")
var ErrLegacyPending = errors.New("legacy import is prepared but its journals and assets are not activated")

type LegacyPreparation struct {
	Account       string                `json:"account"`
	ArchiveDigest string                `json:"archive_digest"`
	Snapshot      legacy.SnapshotReport `json:"snapshot"`
	Ready         bool                  `json:"ready"`
}

// PrepareLegacyImport atomically preserves source evidence in a fresh store.
// It does not claim behavior parity, restore external volumes or activate any
// imported work. Claim is fenced by the incomplete checkpoint. Repeating the
// exact archive returns its first result without resetting subsequent state.
func (s *Store) PrepareLegacyImport(ctx context.Context, bundle *legacy.Bundle, options legacy.SQLiteOptions) (LegacyPreparation, error) {
	var result LegacyPreparation
	if bundle == nil || bundle.Account == "" || len(bundle.Digest) != 64 || len(bundle.SQLite.Digest) != 64 {
		return result, legacy.ErrInvalid
	}
	configuration := bundle.Configuration()
	key := configuration.EncryptionKey()
	fingerprint := sha256.Sum256(key[:])
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
	newIdentity := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !newIdentity {
		return result, err
	}
	if !newIdentity && (saved.AccountID != bundle.Account || saved.KeyFingerprint != hex.EncodeToString(fingerprint[:])) {
		return result, ErrLegacyDestination
	}
	existing, err := q.LegacyImport(ctx, bundle.Account)
	if err == nil {
		if existing.ArchiveDigest != bundle.Digest || existing.SqliteDigest != bundle.SQLite.Digest {
			return result, ErrLegacyDestination
		}
		if json.Unmarshal(existing.Report, &result) != nil || result.Account != bundle.Account || result.ArchiveDigest != bundle.Digest {
			return result, legacy.ErrInvalid
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if err = q.LockQueueTopology(ctx); err != nil {
		return result, err
	}
	if err = emptyLegacyDestination(ctx, tx); err != nil {
		return result, err
	}
	if newIdentity {
		if err = q.CreateIdentity(ctx, db.CreateIdentityParams{AccountID: bundle.Account, KeyFingerprint: hex.EncodeToString(fingerprint[:])}); err != nil {
			return result, err
		}
	}
	bridge, telegram := configuration.Files()
	private, err := json.Marshal(struct{ Bridge, Telegram []byte }{bridge, telegram})
	if err != nil {
		return result, legacy.ErrInvalid
	}
	cipher, err := secrets.New(key)
	if err != nil {
		return result, err
	}
	sealed, err := cipher.Seal(string(private))
	if err != nil {
		return result, err
	}
	manifest, err := json.Marshal(bundle.Manifest)
	if err != nil {
		return result, legacy.ErrInvalid
	}
	if err = q.CreateLegacyImport(ctx, db.CreateLegacyImportParams{AccountID: bundle.Account, ArchiveDigest: bundle.Digest, SqliteDigest: bundle.SQLite.Digest, Manifest: manifest, ConfigurationCiphertext: sealed, Report: []byte(`{}`)}); err != nil {
		return result, err
	}
	rows := make([][]any, 0, 512)
	var rowBytes int
	flush := func() error {
		if len(rows) == 0 {
			return nil
		}
		n, err := tx.CopyFrom(ctx, pgx.Identifier{"legacy_rows"}, []string{"account_id", "ordinal", "source_table", "raw_row"}, pgx.CopyFromRows(rows))
		if err != nil {
			return err
		}
		if n != int64(len(rows)) {
			return legacy.ErrInvalid
		}
		rows = make([][]any, 0, 512)
		rowBytes = 0
		return nil
	}
	report, err := legacy.ReadSnapshot(ctx, bundle, options, func(record legacy.Record) error {
		rows = append(rows, []any{bundle.Account, record.Ordinal, record.Table, []byte(record.Raw)})
		rowBytes += len(record.Raw)
		if len(rows) >= 512 || rowBytes >= 8<<20 {
			return flush()
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if err = flush(); err != nil {
		return result, err
	}
	if err = q.ProjectLegacyValues(ctx, bundle.Account); err != nil {
		return result, err
	}
	result = LegacyPreparation{Account: bundle.Account, ArchiveDigest: bundle.Digest, Snapshot: report}
	raw, _ := json.Marshal(result)
	if err = q.FinishLegacyPreparation(ctx, db.FinishLegacyPreparationParams{AccountID: bundle.Account, Report: raw}); err != nil {
		return LegacyPreparation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return LegacyPreparation{}, err
	}
	return result, nil
}

func emptyLegacyDestination(ctx context.Context, tx pgx.Tx) error {
	catalog, err := tx.Query(ctx, `SELECT tablename::text FROM pg_catalog.pg_tables WHERE schemaname=current_schema()
AND tablename NOT IN ('goose_db_version','bridge_identity') ORDER BY tablename`)
	if err != nil {
		return err
	}
	tables, err := pgx.CollectRows(catalog, pgx.RowTo[string])
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return ErrLegacyDestination
	}
	quoted := make([]string, len(tables))
	for i, name := range tables {
		quoted[i] = pgx.Identifier{name}.Sanitize()
	}
	// NOWAIT refuses concurrent activity rather than waiting behind an unknown
	// writer. Names come from PostgreSQL's catalog, never from archive content.
	if _, err = tx.Exec(ctx, "LOCK TABLE "+strings.Join(quoted, ",")+" IN SHARE ROW EXCLUSIVE MODE NOWAIT"); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrLegacyDestination
	}
	for _, name := range quoted {
		var populated bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+name+" LIMIT 1)").Scan(&populated); err != nil {
			return err
		}
		if populated {
			return ErrLegacyDestination
		}
	}
	return nil
}

// LegacyRuntimeReady is an explicit startup gate in addition to Claim's SQL
// predicate. A source checkpoint alone never proves active journal coverage.
func (s *Store) LegacyRuntimeReady(ctx context.Context, account string) error {
	if account == "" {
		return legacy.ErrInvalid
	}
	pending, err := s.queries.LegacyActivationPending(ctx, account)
	if err != nil {
		return err
	}
	if pending {
		return ErrLegacyPending
	}
	return nil
}
