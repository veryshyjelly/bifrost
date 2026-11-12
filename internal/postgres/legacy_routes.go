package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

type LegacyRouteProjection struct {
	Account            string `json:"account"`
	ArchiveDigest      string `json:"archive_digest"`
	Session            string `json:"session"`
	Routes             int    `json:"routes"`
	Inboxes            int    `json:"inboxes"`
	Retired            int    `json:"retired"`
	PendingRetirements int    `json:"pending_retirements"`
}

// ProjectLegacyRoutes preserves existing route types and deletion choices.
// Session is the explicitly planned provider connection for metadata caches;
// an empty value retains the original connection. Neither changes account ID.
// Owned retirement crashes stay fenced, and no provider operation is invoked.
func (s *Store) ProjectLegacyRoutes(ctx context.Context, account string, key [32]byte, session string) (LegacyRouteProjection, error) {
	var result LegacyRouteProjection
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
	fingerprint := sha256.Sum256(key[:])
	if saved.AccountID != account || saved.KeyFingerprint != hex.EncodeToString(fingerprint[:]) {
		return result, ErrLegacyDestination
	}
	checkpoint, err := q.LockLegacyImport(ctx, account)
	if err != nil {
		return result, err
	}
	configuration, err := legacyConfiguration(checkpoint, key)
	if err != nil {
		return result, err
	}
	if session == "" {
		session = configuration.Value("OPENWA_SESSION_ID")
	}
	if session == "" || len(session) > 256 || strings.TrimSpace(session) != session || strings.ContainsAny(session, "\x00\r\n") {
		return result, legacy.ErrInvalid
	}
	previous, err := q.LegacyRouteProjection(ctx, account)
	if err == nil {
		if json.Unmarshal(previous, &result) != nil || result.Account != account || result.ArchiveDigest != checkpoint.ArchiveDigest {
			return result, legacy.ErrInvalid
		}
		if result.Session != session {
			return LegacyRouteProjection{}, ErrLegacyDestination
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if checkpoint.ActivatedAt.Valid {
		return result, legacy.ErrInvalid
	}
	if _, err = q.LegacyDirectoryProjection(ctx, account); err != nil {
		return result, err
	}
	if err = q.LockContactDirectory(ctx, account); err != nil {
		return result, err
	}
	conflict, err := q.LegacyRouteProjectionConflict(ctx, account)
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
	values, err := q.LegacyRouteValues(ctx, account)
	if err != nil {
		return result, err
	}
	input := make([]legacy.KV, len(values))
	for i, v := range values {
		input[i] = legacy.KV{Kind: v.Kind, ID: v.SourceID, Value: v.SourceValue}
	}
	coordinates, err := q.LegacyTopicCoordinates(ctx, db.LegacyTopicCoordinatesParams{AccountID: account, Management: strconv.FormatInt(configuration.ManagementChatID(), 10)})
	if err != nil {
		return result, err
	}
	evidence := make([]legacy.TopicCoordinate, 0, len(coordinates))
	for _, v := range coordinates {
		chat, chatErr := strconv.ParseInt(v.Chat, 10, 64)
		thread, threadErr := strconv.ParseInt(v.Thread, 10, 64)
		if chatErr == nil && threadErr == nil && chat < 0 && chat >= -9007199254740991 && thread > 1 && thread <= 2147483647 {
			evidence = append(evidence, legacy.TopicCoordinate{Chat: chat, Thread: thread})
		}
	}
	projected, err := legacy.ProjectRoutes(configuration, input, directory, evidence)
	if err != nil {
		return result, err
	}
	result = LegacyRouteProjection{Account: account, ArchiveDigest: checkpoint.ArchiveDigest, Session: session, Routes: len(projected.Routes), Inboxes: len(projected.Inboxes), Retired: len(projected.Retired)}
	retired := map[legacy.TopicCoordinate]bool{}
	inboxesByChat := make(map[string][]legacy.TopicCoordinate)
	for _, topic := range projected.Retired {
		if err = q.ReserveRetirementTopic(ctx, db.ReserveRetirementTopicParams{AccountID: account, TelegramChatID: topic.Target.Chat, ThreadID: topic.Target.Thread, WhatsappChat: topic.ChatID}); err != nil {
			return LegacyRouteProjection{}, err
		}
		if err = q.DeleteRetirementTopic(ctx, db.DeleteRetirementTopicParams{AccountID: account, TelegramChatID: topic.Target.Chat, ThreadID: topic.Target.Thread}); err != nil {
			return LegacyRouteProjection{}, err
		}
		retired[topic.Target] = true
	}
	for _, inbox := range projected.Inboxes {
		inboxesByChat[inbox.ChatID] = append(inboxesByChat[inbox.ChatID], inbox.Target)
		if err = q.RememberInbox(ctx, db.RememberInboxParams{AccountID: account, WhatsappChat: inbox.ChatID, TelegramChatID: inbox.Target.Chat, ThreadID: inbox.Target.Thread}); err != nil {
			return LegacyRouteProjection{}, err
		}
	}
	for _, state := range projected.Routes {
		r := state.Route
		if _, err = q.CreateInbox(ctx, db.CreateInboxParams{AccountID: account, WhatsappChat: r.ChatID, Name: r.Name, TelegramChatID: r.TelegramChatID, ThreadID: r.ThreadID, Mode: string(r.Mode), InboxRetired: r.InboxRetired}); err != nil {
			return LegacyRouteProjection{}, err
		}
		if state.ReturnKey != "" {
			if err = q.SaveMirrorReturn(ctx, db.SaveMirrorReturnParams{AccountID: account, WhatsappChat: r.ChatID, ReturnKey: state.ReturnKey}); err != nil {
				return LegacyRouteProjection{}, err
			}
		}
		if state.PendingRetirement {
			result.PendingRetirements++
		}
		for _, inbox := range inboxesByChat[r.ChatID] {
			if inbox.Chat != configuration.ManagementChatID() || retired[inbox] {
				continue
			}
			if err = q.RememberMetadataTopicName(ctx, db.RememberMetadataTopicNameParams{AccountID: account, SessionID: session, SourceID: r.ChatID, ChatID: inbox.Chat, ThreadID: inbox.Thread, ReturnKey: state.ReturnKey, Name: state.TopicName}); err != nil {
				return LegacyRouteProjection{}, err
			}
		}
	}
	raw, _ := json.Marshal(result)
	if err = q.SaveLegacyRouteProjection(ctx, db.SaveLegacyRouteProjectionParams{AccountID: account, Result: raw}); err != nil {
		return LegacyRouteProjection{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return LegacyRouteProjection{}, err
	}
	return result, nil
}
