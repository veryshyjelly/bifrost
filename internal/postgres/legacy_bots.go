package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/secrets"
)

type LegacyBotProjection struct {
	Account       string `json:"account"`
	ArchiveDigest string `json:"archive_digest"`
	Credentials   int    `json:"credentials"`
	HostedLogouts int    `json:"hosted_logouts"`
}

// ProjectLegacyBots converts the credential capability within a prepared
// account. It does not activate the import or seed local API eligibility.
// Its first result and logout proofs are immutable, making replay harmless.
func (s *Store) ProjectLegacyBots(ctx context.Context, account string, key [32]byte) (LegacyBotProjection, error) {
	var result LegacyBotProjection
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
	identity, err := q.Identity(ctx)
	if err != nil {
		return result, err
	}
	fingerprint := sha256.Sum256(key[:])
	if identity.AccountID != account || identity.KeyFingerprint != hex.EncodeToString(fingerprint[:]) {
		return result, ErrLegacyDestination
	}
	checkpoint, err := q.LockLegacyImport(ctx, account)
	if err != nil {
		return result, err
	}
	previous, err := q.LegacyBotProjection(ctx, account)
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
	if err = q.LockContactDirectory(ctx, account); err != nil {
		return result, err
	}
	conflict, err := q.LegacyBotProjectionConflict(ctx, account)
	if err != nil {
		return result, err
	}
	if conflict {
		return result, ErrLegacyDestination
	}
	cipher, err := secrets.New(key)
	if err != nil {
		return result, err
	}
	private, err := cipher.Open(checkpoint.ConfigurationCiphertext)
	if err != nil {
		return result, legacy.ErrInvalid
	}
	var files struct{ Bridge, Telegram []byte }
	if json.Unmarshal([]byte(private), &files) != nil {
		return result, legacy.ErrInvalid
	}
	configuration, err := legacy.ParseConfiguration(files.Bridge, files.Telegram)
	if err != nil || configuration.EncryptionKey() != key || (configuration.Value("BIFROST_ACCOUNT_ID") != "" && configuration.Value("BIFROST_ACCOUNT_ID") != account) {
		return result, legacy.ErrInvalid
	}
	values, err := q.LegacyBotValues(ctx, account)
	if err != nil {
		return result, err
	}
	input := make([]legacy.KV, len(values))
	for i, v := range values {
		input[i] = legacy.KV{Kind: v.Kind, ID: v.SourceID, Value: v.SourceValue}
	}
	projected, err := legacy.ProjectBots(configuration, input)
	if err != nil {
		return result, err
	}
	result = LegacyBotProjection{Account: account, ArchiveDigest: checkpoint.ArchiveDigest, Credentials: len(projected)}
	for _, state := range projected {
		credential := state.Credential
		if err = saveCredential(ctx, q, account, credential); err != nil {
			return LegacyBotProjection{}, err
		}
		if credential.BotID != "master" {
			if err = q.PutIngressBot(ctx, db.PutIngressBotParams{AccountID: account, BotID: credential.BotID, Username: credential.Identity.Username}); err != nil {
				return LegacyBotProjection{}, err
			}
			if state.Started {
				if _, err = q.SetBotStarted(ctx, db.SetBotStartedParams{AccountID: account, BotID: credential.BotID}); err != nil {
					return LegacyBotProjection{}, err
				}
			}
		}
		if state.HostedLogout {
			if err = q.SaveLegacyBotLogout(ctx, db.SaveLegacyBotLogoutParams{AccountID: account, BotID: credential.BotID, CredentialRevision: credential.Revision}); err != nil {
				return LegacyBotProjection{}, err
			}
			result.HostedLogouts++
		}
	}
	raw, _ := json.Marshal(result)
	if err = q.SaveLegacyBotProjection(ctx, db.SaveLegacyBotProjectionParams{AccountID: account, Result: raw}); err != nil {
		return LegacyBotProjection{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return LegacyBotProjection{}, err
	}
	return result, nil
}
