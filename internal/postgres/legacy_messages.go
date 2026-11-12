package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

type LegacyMessageProjection struct {
	Account          string `json:"account"`
	ArchiveDigest    string `json:"archive_digest"`
	Mappings         int    `json:"mappings"`
	Observations     int    `json:"observations"`
	ProviderReceipts int    `json:"provider_receipts"`
	MappingOnly      int    `json:"mapping_only"`
	SelfIdentity     bool   `json:"self_identity"`
}

// ProjectLegacyMessages imports scoped mappings and actual receipt/observation
// evidence. It never creates effects or jobs. Sequence changes are transactional
// and do not rewind an existing counter. Account activation remains blocked.
func (s *Store) ProjectLegacyMessages(ctx context.Context, account string, key [32]byte) (LegacyMessageProjection, error) {
	var result LegacyMessageProjection
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
	if !legacyKeyMatches(saved, account, key) {
		return result, ErrLegacyDestination
	}
	checkpoint, err := q.LockLegacyImport(ctx, account)
	if err != nil {
		return result, err
	}
	previous, err := q.LegacyMessageProjection(ctx, account)
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
	if _, err = q.LegacyCompletionProjection(ctx, account); err != nil {
		return result, err
	}
	if err = q.LockContactDirectory(ctx, account); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, "LOCK TABLE message_mappings,telegram_observations IN SHARE ROW EXCLUSIVE MODE NOWAIT"); err != nil {
		return result, ErrLegacyDestination
	}
	conflict, err := q.LegacyMessageConflict(ctx, account)
	if err != nil {
		return result, err
	}
	if conflict {
		return result, ErrLegacyDestination
	}
	configuration, err := legacyConfiguration(checkpoint, key)
	if err != nil {
		return result, err
	}
	directory, err := directory(ctx, q, account)
	if err != nil {
		return result, err
	}
	values, err := q.LegacyMessageValues(ctx, account)
	if err != nil {
		return result, err
	}
	var input legacy.MessageEvidence
	for _, v := range values {
		input.Values = append(input.Values, legacy.KV{Kind: v.Kind, ID: v.SourceID, Value: v.SourceValue})
	}
	rows, err := q.LegacyMessageRows(ctx, account)
	if err != nil {
		return result, err
	}
	for _, row := range rows {
		switch row.SourceTable {
		case "jobs":
			var v legacy.Job
			if json.Unmarshal(row.RawRow, &v) != nil {
				return result, legacy.ErrInvalid
			}
			input.Jobs = append(input.Jobs, v)
		case "effects":
			var v legacy.Effect
			if json.Unmarshal(row.RawRow, &v) != nil {
				return result, legacy.ErrInvalid
			}
			input.Effects = append(input.Effects, v)
		case "messages":
			var v legacy.Message
			if json.Unmarshal(row.RawRow, &v) != nil {
				return result, legacy.ErrInvalid
			}
			input.Mappings = append(input.Mappings, v)
		case "telegram_messages":
			var v legacy.TelegramMessage
			if json.Unmarshal(row.RawRow, &v) != nil {
				return result, legacy.ErrInvalid
			}
			input.Observations = append(input.Observations, v)
		default:
			return result, legacy.ErrInvalid
		}
	}
	projected, err := legacy.ProjectMessages(configuration, account, directory, input)
	if err != nil {
		return result, err
	}
	var maxObservation int64
	for _, row := range projected.Observations {
		o := row.Observation
		chat, _ := strconv.ParseInt(o.Chat, 10, 64)
		cleanupError := ""
		if o.Error != nil {
			cleanupError = *o.Error
		}
		thread := o.Thread
		if thread == 1 {
			thread = 0
		}
		if err = q.InsertLegacyObservation(ctx, db.InsertLegacyObservationParams{Seq: o.Seq, AccountID: account, Scope: o.Scope, BotID: o.Bot, ChatID: chat, MessageID: o.Message, ThreadID: thread, Kind: o.Kind, MessageDate: o.Date, Payload: row.Payload, Deleted: o.Deleted == 1, ExpiresAt: o.Expires, CleanupError: cleanupError}); err != nil {
			return LegacyMessageProjection{}, err
		}
		maxObservation = max(maxObservation, o.Seq)
	}
	firstMapping, err := identitySequenceNext(ctx, tx, "message_mappings")
	if err != nil {
		return LegacyMessageProjection{}, err
	}
	if int64(len(projected.Mappings)) > math.MaxInt64-firstMapping {
		return LegacyMessageProjection{}, legacy.ErrInvalid
	}
	for i, row := range projected.Mappings {
		m := row.Message
		chat, _ := strconv.ParseInt(m.TGChat, 10, 64)
		scope := m.Bot
		if chat < 0 {
			scope = "*"
		}
		format, _ := json.Marshal(row.Presentation)
		var mentioned []byte
		if row.MentionedIDs != nil {
			mentioned, _ = json.Marshal(row.MentionedIDs)
		}
		if err = q.InsertLegacyMapping(ctx, db.InsertLegacyMappingParams{Seq: firstMapping + int64(i), AccountID: account, Scope: scope, TelegramChatID: chat, TelegramMessageID: m.TGID, ThreadID: row.Thread, BotID: m.Bot, WhatsappChat: m.WAChat, WhatsappMessageID: m.WAID, Direction: m.Direction, Receipt: row.Receipt, Media: m.Media == 1, Presentation: format, MentionedIds: mentioned, SourcePayload: row.SourcePayload, ReceiptOrigin: row.ReceiptOrigin}); err != nil {
			return LegacyMessageProjection{}, err
		}
		if row.ReceiptOrigin == "mapping-only" {
			result.MappingOnly++
		} else {
			result.ProviderReceipts++
		}
	}
	retired, err := q.RetiredObservationTopics(ctx, account)
	if err != nil {
		return LegacyMessageProjection{}, err
	}
	for _, topic := range retired {
		if err = q.MarkRetiredTopicObservations(ctx, db.MarkRetiredTopicObservationsParams{AccountID: account, ChatID: topic.TelegramChatID, ThreadID: topic.ThreadID}); err != nil {
			return LegacyMessageProjection{}, err
		}
	}
	if projected.SelfID != "" {
		got, err := q.SaveSelfIdentity(ctx, db.SaveSelfIdentityParams{AccountID: account, PersonID: projected.SelfID})
		if err != nil {
			return LegacyMessageProjection{}, err
		}
		if got != projected.SelfID {
			return LegacyMessageProjection{}, legacy.ErrInvalid
		}
	}
	if maxObservation >= math.MaxInt64-1 {
		return LegacyMessageProjection{}, legacy.ErrInvalid
	}
	if err = advanceIdentitySequence(ctx, tx, "telegram_observations", maxObservation+1); err != nil {
		return LegacyMessageProjection{}, err
	}
	if err = advanceIdentitySequence(ctx, tx, "message_mappings", firstMapping+int64(len(projected.Mappings))); err != nil {
		return LegacyMessageProjection{}, err
	}
	result.Account, result.ArchiveDigest = account, checkpoint.ArchiveDigest
	result.Mappings, result.Observations, result.SelfIdentity = len(projected.Mappings), len(projected.Observations), projected.SelfID != ""
	raw, _ := json.Marshal(result)
	if err = q.SaveLegacyMessageProjection(ctx, db.SaveLegacyMessageProjectionParams{AccountID: account, Result: raw}); err != nil {
		return LegacyMessageProjection{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return LegacyMessageProjection{}, err
	}
	return result, nil
}

func identitySequenceName(ctx context.Context, tx pgx.Tx, table string) (string, error) {
	var schema, name string
	err := tx.QueryRow(ctx, `SELECT n.nspname,c.relname FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
WHERE c.oid=pg_get_serial_sequence($1,'seq')::regclass`, table).Scan(&schema, &name)
	if err != nil {
		return "", err
	}
	return pgx.Identifier{schema, name}.Sanitize(), nil
}

func identitySequenceNext(ctx context.Context, tx pgx.Tx, table string) (int64, error) {
	name, err := identitySequenceName(ctx, tx, table)
	if err != nil {
		return 0, err
	}
	var last int64
	var called bool
	if err = tx.QueryRow(ctx, "SELECT last_value,is_called FROM "+name).Scan(&last, &called); err != nil {
		return 0, err
	}
	if called {
		if last == math.MaxInt64 {
			return 0, legacy.ErrInvalid
		}
		last++
	}
	return last, nil
}

func advanceIdentitySequence(ctx context.Context, tx pgx.Tx, table string, next int64) error {
	current, err := identitySequenceNext(ctx, tx, table)
	if err != nil {
		return err
	}
	next = max(current, next)
	name, err := identitySequenceName(ctx, tx, table)
	if err != nil {
		return err
	}
	// ALTER SEQUENCE RESTART rolls back with the import. setval does not.
	_, err = tx.Exec(ctx, "ALTER SEQUENCE "+name+" RESTART WITH "+strconv.FormatInt(next, 10))
	return err
}
