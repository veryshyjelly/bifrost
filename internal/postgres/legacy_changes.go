package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

type LegacyChangeProjection struct {
	Account       string `json:"account"`
	ArchiveDigest string `json:"archive_digest"`
	Edits         int    `json:"edits"`
	Outbound      int    `json:"outbound"`
	Revoked       int    `json:"revoked"`
}

// ProjectLegacyChanges restores saved message state without scheduling new
// edits, deleting copies, creating provider receipts or activating delivery.
func (s *Store) ProjectLegacyChanges(ctx context.Context, account string) (LegacyChangeProjection, error) {
	var result LegacyChangeProjection
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
	previous, err := q.LegacyChangeProjection(ctx, account)
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
	if _, err = q.LegacySourceProjection(ctx, account); err != nil {
		return result, err
	}
	if err = q.LockContactDirectory(ctx, account); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, "LOCK TABLE retained_message_edits,message_latest_edits,message_outbound_edits,message_tombstones IN SHARE ROW EXCLUSIVE MODE NOWAIT"); err != nil {
		return result, err
	}
	conflict, err := q.LegacyChangeConflict(ctx, account)
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
	values, err := q.LegacyChangeValues(ctx, account)
	if err != nil {
		return result, err
	}
	input := make([]legacy.KV, len(values))
	for i, value := range values {
		input[i] = legacy.KV{Kind: value.Kind, ID: value.SourceID, Value: value.SourceValue}
	}
	state, err := legacy.ProjectChanges(input, directory)
	if err != nil {
		return result, err
	}
	for _, edit := range state.Edits {
		var mentioned json.RawMessage
		if edit.MentionedIDs != nil {
			mentioned, _ = json.Marshal(edit.MentionedIDs)
		}
		if err = q.SaveRetainedMessageEdit(ctx, db.SaveRetainedMessageEditParams{AccountID: account, WhatsappChat: edit.ChatID, MessageID: edit.MessageID, SourceKey: edit.SourceKey, Body: edit.Body, MentionedIds: mentioned}); err != nil {
			return LegacyChangeProjection{}, err
		}
	}
	for _, edit := range state.Outbound {
		if err = q.SaveOutboundEdit(ctx, db.SaveOutboundEditParams{AccountID: account, WhatsappChat: edit.ChatID, MessageID: edit.MessageID, BodyHash: edit.BodyHash}); err != nil {
			return LegacyChangeProjection{}, err
		}
	}
	for _, revoked := range state.Revoked {
		if err = q.SetMessageRevoked(ctx, db.SetMessageRevokedParams{AccountID: account, WhatsappChat: revoked.ChatID, MessageID: revoked.MessageID}); err != nil {
			return LegacyChangeProjection{}, err
		}
	}
	result = LegacyChangeProjection{Account: account, ArchiveDigest: checkpoint.ArchiveDigest, Edits: len(state.Edits), Outbound: len(state.Outbound), Revoked: len(state.Revoked)}
	raw, _ := json.Marshal(result)
	if err = q.SaveLegacyChangeProjection(ctx, db.SaveLegacyChangeProjectionParams{AccountID: account, Result: raw}); err != nil {
		return LegacyChangeProjection{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return LegacyChangeProjection{}, err
	}
	return result, nil
}
