package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/retained"
	"github.com/veryshyjelly/bifrost/internal/secrets"
)

var ErrRetainedPending = errors.New("retained account state is incomplete; restore a fully verified database and assets")

// RetainedStateReady keeps incomplete restored state fenced even though archive
// preparation and activation tooling is no longer part of the runtime binary.
func (s *Store) RetainedStateReady(ctx context.Context, account string) error {
	if account == "" {
		return retained.ErrInvalid
	}
	pending, err := s.queries.RetainedActivationPending(ctx, account)
	if err != nil {
		return err
	}
	if pending {
		return ErrRetainedPending
	}
	return nil
}

// RetainedRuntimeConfiguration verifies original encrypted identity settings.
// Schema identifiers are intentionally preserved in the read-only SQL adapter.
func (s *Store) RetainedRuntimeConfiguration(ctx context.Context, account string, key [32]byte) (retained.Configuration, bool, error) {
	checkpoint, err := s.queries.RetainedRuntimeConfiguration(ctx, account)
	if errors.Is(err, pgx.ErrNoRows) {
		return retained.Configuration{}, false, nil
	}
	if err != nil {
		return retained.Configuration{}, false, err
	}
	cipher, err := secrets.New(key)
	if err != nil {
		return retained.Configuration{}, false, err
	}
	private, err := cipher.Open(checkpoint.ConfigurationCiphertext)
	if err != nil {
		return retained.Configuration{}, false, retained.ErrInvalid
	}
	var files struct{ Bridge, Telegram []byte }
	if json.Unmarshal([]byte(private), &files) != nil {
		return retained.Configuration{}, false, retained.ErrInvalid
	}
	configuration, err := retained.ParseConfiguration(files.Bridge, files.Telegram)
	if err != nil || configuration.EncryptionKey() != key || (configuration.Value("BIFROST_ACCOUNT_ID") != "" && configuration.Value("BIFROST_ACCOUNT_ID") != checkpoint.AccountID) {
		return retained.Configuration{}, false, retained.ErrInvalid
	}
	return configuration, true, nil
}

func (s *Store) MessageComplete(ctx context.Context, account, chat, id string) (bool, error) {
	return s.queries.MessageComplete(ctx, db.MessageCompleteParams{AccountID: account, WhatsappChat: chat, MessageID: id})
}
