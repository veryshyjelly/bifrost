package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/statuses"
)

type LegacyStatusProjection struct {
	Account       string `json:"account"`
	ArchiveDigest string `json:"archive_digest"`
	Feed          bool   `json:"feed"`
	Mutes         int    `json:"mutes"`
	Skipped       int    `json:"skipped"`
	Operations    int    `json:"operations"`
	Revision      int64  `json:"revision"`
}

func (s *Store) ProjectLegacyStatuses(ctx context.Context, account string, key [32]byte) (LegacyStatusProjection, error) {
	var result LegacyStatusProjection
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
	previous, err := q.LegacyStatusProjection(ctx, account)
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
	if _, err = q.LegacyIncomingReactionProjection(ctx, account); err != nil {
		return result, err
	}
	configuration, err := legacyConfiguration(checkpoint, key)
	if err != nil {
		return result, err
	}
	if err = q.LockContactDirectory(ctx, account); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, "LOCK TABLE status_feeds,retained_status_feeds,status_mutes,status_mute_counters,status_mute_operations,retained_status_skips,retained_status_mute_operations IN SHARE ROW EXCLUSIVE MODE NOWAIT"); err != nil {
		return result, err
	}
	conflict, err := q.LegacyStatusConflict(ctx, account)
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
	values, err := q.LegacyStatusValues(ctx, account)
	if err != nil {
		return result, err
	}
	input := make([]legacy.KV, len(values))
	for i, v := range values {
		input[i] = legacy.KV{Kind: v.Kind, ID: v.SourceID, Value: v.SourceValue}
	}
	rows, err := q.LegacyStatusJobs(ctx, account)
	if err != nil {
		return result, err
	}
	jobs := make([]legacy.Job, len(rows))
	for i, raw := range rows {
		if json.Unmarshal(raw, &jobs[i]) != nil {
			return result, legacy.ErrInvalid
		}
	}
	state, err := legacy.ProjectStatuses(account, configuration, input, jobs, directory)
	if err != nil {
		return result, err
	}
	if state.Feed != nil {
		feed := state.Feed.Feed
		reserved, err := q.ReservedTopic(ctx, db.ReservedTopicParams{AccountID: account, TelegramChatID: feed.Management, ThreadID: feed.ThreadID})
		if err != nil {
			return result, err
		}
		if reserved {
			return result, legacy.ErrTopicEvidence
		}
		other, err := q.OtherTopicRoute(ctx, db.OtherTopicRouteParams{AccountID: account, TelegramChatID: feed.Management, ThreadID: feed.ThreadID, WhatsappChat: statuses.ChatID})
		if err != nil {
			return result, err
		}
		if other {
			return result, legacy.ErrTopicEvidence
		}
		raw, _ := json.Marshal(state.Feed)
		if err = q.SaveRetainedStatusFeed(ctx, db.SaveRetainedStatusFeedParams{AccountID: account, Management: feed.Management, SourceKey: strings.TrimPrefix(state.Feed.Key, "status-topic:"), Payload: raw}); err != nil {
			return LegacyStatusProjection{}, err
		}
	}
	for _, mute := range state.Mutes {
		raw, _ := json.Marshal(mute)
		if err = q.SaveStatusMute(ctx, db.SaveStatusMuteParams{AccountID: account, Author: mute.Author, Payload: raw}); err != nil {
			return LegacyStatusProjection{}, err
		}
	}
	if state.Revision > 0 {
		if err = q.SaveLegacyStatusCounter(ctx, db.SaveLegacyStatusCounterParams{AccountID: account, Revision: state.Revision}); err != nil {
			return LegacyStatusProjection{}, err
		}
	}
	for _, skip := range state.Skipped {
		if err = q.SaveRetainedStatusSkip(ctx, db.SaveRetainedStatusSkipParams{AccountID: account, SourceKey: skip.SourceKey, Author: skip.Author, SkippedAt: skip.At}); err != nil {
			return LegacyStatusProjection{}, err
		}
	}
	for _, operation := range state.Operations {
		raw, _ := json.Marshal(operation.Mute)
		if err = q.SaveRetainedStatusMuteOperation(ctx, db.SaveRetainedStatusMuteOperationParams{AccountID: account, LegacyJobKey: operation.JobKey, SourceKey: operation.SourceKey, Payload: raw}); err != nil {
			return LegacyStatusProjection{}, err
		}
	}
	result = LegacyStatusProjection{Account: account, ArchiveDigest: checkpoint.ArchiveDigest, Feed: state.Feed != nil, Mutes: len(state.Mutes), Skipped: len(state.Skipped), Operations: len(state.Operations), Revision: state.Revision}
	raw, _ := json.Marshal(result)
	if err = q.SaveLegacyStatusProjection(ctx, db.SaveLegacyStatusProjectionParams{AccountID: account, Result: raw}); err != nil {
		return LegacyStatusProjection{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return LegacyStatusProjection{}, err
	}
	return result, nil
}
