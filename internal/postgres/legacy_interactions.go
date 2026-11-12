package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

type LegacyInteractionProjection struct {
	Account       string `json:"account"`
	ArchiveDigest string `json:"archive_digest"`
	Polls         int    `json:"polls"`
	Custom        int    `json:"custom"`
	Choices       int    `json:"choices"`
}

// ProjectLegacyInteractions restores selected options and frozen emoji
// resolutions. Neither proves an unconfirmed operation reached a provider.
func (s *Store) ProjectLegacyInteractions(ctx context.Context, account string) (LegacyInteractionProjection, error) {
	var result LegacyInteractionProjection
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
	previous, err := q.LegacyInteractionProjection(ctx, account)
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
	if _, err = q.LegacyChangeProjection(ctx, account); err != nil {
		return result, err
	}
	if err = q.LockContactDirectory(ctx, account); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, "LOCK TABLE poll_selections,custom_reaction_emojis,retained_reaction_choices,outgoing_reaction_plans IN SHARE ROW EXCLUSIVE MODE NOWAIT"); err != nil {
		return result, err
	}
	conflict, err := q.LegacyInteractionConflict(ctx, account)
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
	values, err := q.LegacyInteractionValues(ctx, account)
	if err != nil {
		return result, err
	}
	input := make([]legacy.KV, len(values))
	for i, value := range values {
		input[i] = legacy.KV{Kind: value.Kind, ID: value.SourceID, Value: value.SourceValue}
	}
	state, err := legacy.ProjectInteractions(input, directory)
	if err != nil {
		return result, err
	}
	for _, poll := range state.Polls {
		raw, _ := json.Marshal(poll.Options)
		if err = q.SetPollSelection(ctx, db.SetPollSelectionParams{AccountID: account, WhatsappChat: poll.ChatID, MessageID: poll.MessageID, Options: raw}); err != nil {
			return LegacyInteractionProjection{}, err
		}
	}
	for _, custom := range state.Custom {
		if err = q.SaveCustomReactionEmoji(ctx, db.SaveCustomReactionEmojiParams{AccountID: account, CustomID: custom.ID, Emoji: custom.Emoji}); err != nil {
			return LegacyInteractionProjection{}, err
		}
	}
	for _, choice := range state.Choices {
		if err = q.SaveRetainedReactionChoice(ctx, db.SaveRetainedReactionChoiceParams{AccountID: account, SourceKey: choice.SourceKey, Emoji: choice.Choice.Emoji, Supported: choice.Choice.Supported}); err != nil {
			return LegacyInteractionProjection{}, err
		}
	}
	result = LegacyInteractionProjection{Account: account, ArchiveDigest: checkpoint.ArchiveDigest, Polls: len(state.Polls), Custom: len(state.Custom), Choices: len(state.Choices)}
	raw, _ := json.Marshal(result)
	if err = q.SaveLegacyInteractionProjection(ctx, db.SaveLegacyInteractionProjectionParams{AccountID: account, Result: raw}); err != nil {
		return LegacyInteractionProjection{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return LegacyInteractionProjection{}, err
	}
	return result, nil
}
