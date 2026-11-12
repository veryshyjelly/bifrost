package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

type LegacyIncomingReactionProjection struct {
	Account         string `json:"account"`
	ArchiveDigest   string `json:"archive_digest"`
	States          int    `json:"states"`
	Echoes          int    `json:"echoes"`
	Caches          int    `json:"caches"`
	PendingFallback int    `json:"pending_fallback"`
}

// ProjectLegacyIncomingReactions preserves confirmed settings and source actor
// facts independently of unfinished original reaction/notice job orchestration.
func (s *Store) ProjectLegacyIncomingReactions(ctx context.Context, account string) (LegacyIncomingReactionProjection, error) {
	var result LegacyIncomingReactionProjection
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
	previous, err := q.LegacyIncomingReactionProjection(ctx, account)
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
	if _, err = q.LegacyInteractionProjection(ctx, account); err != nil {
		return result, err
	}
	if err = q.LockContactDirectory(ctx, account); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, "LOCK TABLE message_mappings,message_reactions,reaction_destinations,reaction_echoes,reaction_plans,outgoing_reaction_plans,legacy_pending_reaction_notices IN SHARE ROW EXCLUSIVE MODE NOWAIT"); err != nil {
		return result, err
	}
	conflict, err := q.LegacyIncomingReactionConflict(ctx, account)
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
	var evidence legacy.ReactionEvidence
	values, err := q.LegacyIncomingReactionValues(ctx, account)
	if err != nil {
		return result, err
	}
	for _, value := range values {
		evidence.Values = append(evidence.Values, legacy.KV{Kind: value.Kind, ID: value.SourceID, Value: value.SourceValue})
	}
	botRows, err := q.IdentityBots(ctx, account)
	if err != nil {
		return result, err
	}
	for _, bot := range botRows {
		evidence.Bots = append(evidence.Bots, bot.BotID)
	}
	locations, err := q.LegacyIncomingReactionCopies(ctx, account)
	if err != nil {
		return result, err
	}
	type copyKey struct {
		bot           string
		chat, message int64
	}
	byCopy := make(map[copyKey]db.LegacyIncomingReactionCopiesRow, len(locations))
	for _, location := range locations {
		byCopy[copyKey{location.BotID, location.TelegramChatID, location.TelegramMessageID}] = location
	}
	rows, err := q.LegacyIncomingReactionRows(ctx, account)
	if err != nil {
		return result, err
	}
	for _, row := range rows {
		if row.SourceTable == "jobs" {
			var job legacy.Job
			if json.Unmarshal(row.RawRow, &job) != nil {
				return result, legacy.ErrInvalid
			}
			evidence.Jobs = append(evidence.Jobs, job)
			continue
		}
		var message legacy.Message
		if json.Unmarshal(row.RawRow, &message) != nil {
			return result, legacy.ErrInvalid
		}
		chat, err := strconv.ParseInt(message.TGChat, 10, 64)
		if err != nil {
			return result, legacy.ErrInvalid
		}
		location, exists := byCopy[copyKey{message.Bot, chat, message.TGID}]
		if !exists || location.WhatsappChat != directory.Canonical(message.WAChat) || location.WhatsappMessageID != message.WAID {
			return result, legacy.ErrReactionEvidence
		}
		evidence.Copies = append(evidence.Copies, legacy.ReactionCopy{RawChatID: message.WAChat, MessageID: message.WAID, BotID: message.Bot, TelegramChat: chat, TelegramMessage: message.TGID, TelegramThread: location.ThreadID})
	}
	state, err := legacy.ProjectIncomingReactions(account, evidence, directory)
	if err != nil {
		return result, err
	}
	for _, saved := range state.States {
		raw, _ := json.Marshal(saved.State)
		if err = q.SaveReactionState(ctx, db.SaveReactionStateParams{AccountID: account, WhatsappChat: saved.ChatID, MessageID: saved.MessageID, Payload: raw}); err != nil {
			return LegacyIncomingReactionProjection{}, err
		}
	}
	for _, saved := range state.Echoes {
		raw, _ := json.Marshal(saved.Echo)
		if err = q.SaveReactionEcho(ctx, db.SaveReactionEchoParams{AccountID: account, WhatsappChat: saved.ChatID, MessageID: saved.MessageID, Payload: raw}); err != nil {
			return LegacyIncomingReactionProjection{}, err
		}
	}
	for _, saved := range state.Caches {
		raw, _ := json.Marshal(saved.Cache)
		if err = q.SaveReactionCache(ctx, db.SaveReactionCacheParams{AccountID: account, WhatsappChat: saved.ChatID, MessageID: saved.MessageID, DestinationKey: saved.DestinationKey, Payload: raw}); err != nil {
			return LegacyIncomingReactionProjection{}, err
		}
		if saved.PendingFallback != "" {
			if err = q.SaveLegacyPendingReactionNotice(ctx, db.SaveLegacyPendingReactionNoticeParams{AccountID: account, SourceKey: saved.SourceKey, WhatsappChat: saved.ChatID, MessageID: saved.MessageID, DestinationKey: saved.DestinationKey, LegacyJobKey: saved.PendingFallback}); err != nil {
				return LegacyIncomingReactionProjection{}, err
			}
			result.PendingFallback++
		}
	}
	result.Account, result.ArchiveDigest = account, checkpoint.ArchiveDigest
	result.States, result.Echoes, result.Caches = len(state.States), len(state.Echoes), len(state.Caches)
	raw, _ := json.Marshal(result)
	if err = q.SaveLegacyIncomingReactionProjection(ctx, db.SaveLegacyIncomingReactionProjectionParams{AccountID: account, Result: raw}); err != nil {
		return LegacyIncomingReactionProjection{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return LegacyIncomingReactionProjection{}, err
	}
	return result, nil
}
