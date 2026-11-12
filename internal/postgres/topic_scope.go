package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/go-telegram/bot"
	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/recovery"
	"github.com/veryshyjelly/bifrost/internal/relay"
	"github.com/veryshyjelly/bifrost/internal/statuses"
)

type topicScope struct {
	kind, chat, key, name string
	management            int64
	guard                 bool
	transition            *control.Transition
	native                *relay.NativePlan
}

func topicScopeFor(ctx context.Context, q *db.Queries, row db.Job) (topicScope, error) {
	if row.Kind == "management" {
		raw, err := q.MirrorTransition(ctx, db.MirrorTransitionParams{AccountID: row.AccountID, JobID: row.ID})
		if err == nil {
			var p control.Transition
			if json.Unmarshal(raw, &p) != nil {
				return topicScope{}, delivery.ErrInvalid
			}
			if p.Kind == "return_to_inbox" && p.Stage == "create" {
				return topicScope{kind: "mirror-return", chat: p.ChatID, key: row.Dedup + ":topic", name: conversation.TopicName(p.Name, p.ChatID), management: p.Management, guard: true, transition: &p}, nil
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return topicScope{}, err
		}
	}
	if row.Kind == "wa" {
		plans, err := q.TopicReviewNativePlans(ctx, db.TopicReviewNativePlansParams{AccountID: row.AccountID, JobID: row.ID})
		if err != nil {
			return topicScope{}, err
		}
		for _, plan := range plans {
			var p relay.NativePlan
			if json.Unmarshal(plan.Payload, &p) != nil {
				return topicScope{}, delivery.ErrInvalid
			}
			if p.Repair != nil && p.Repair.CreateTopic {
				if p.Format != "native-packets-v1" || p.Origin != "live" || p.Direction != "in" || p.Key != relay.NativeIncomingKey(p.WhatsAppChat, p.WhatsAppMessage) || plan.Complete {
					return topicScope{}, delivery.ErrInvalid
				}
				return topicScope{kind: "native-repair", chat: p.WhatsAppChat, key: p.Repair.TopicKey, name: p.Repair.TopicName, management: p.Repair.Management, guard: true, native: &p}, nil
			}
		}
	}
	feeds, err := q.StatusTopicOrigins(ctx, db.StatusTopicOriginsParams{AccountID: row.AccountID, JobID: row.ID})
	if err != nil {
		return topicScope{}, err
	}
	if len(feeds) > 1 {
		return topicScope{}, delivery.ErrInvalid
	}
	if len(feeds) == 1 && !feeds[0].Complete {
		var p statuses.Topic
		if json.Unmarshal(feeds[0].Payload, &p) != nil || p.Feed.Management != feeds[0].Management || p.Feed.ThreadID != 0 {
			return topicScope{}, delivery.ErrInvalid
		}
		return topicScope{kind: "status", chat: statuses.ChatID, key: p.Key, name: p.Name, management: p.Feed.Management}, nil
	}
	if row.Kind == "wa" {
		source, err := q.WhatsAppSource(ctx, db.WhatsAppSourceParams{AccountID: row.AccountID, JobID: row.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return topicScope{}, delivery.ErrInvalid
		}
		if err != nil {
			return topicScope{}, err
		}
		var p identity.Source
		if json.Unmarshal(source.Payload, &p) != nil || p.ChatID == "" || row.Stream != "wa:"+p.ChatID {
			return topicScope{}, delivery.ErrInvalid
		}
		return topicScope{kind: "inbox", chat: p.ChatID, key: row.Dedup + ":topic"}, nil
	}
	return topicScope{}, delivery.ErrInvalid
}

func topicScopeBlocked(ctx context.Context, q *db.Queries, row db.Job, scope topicScope) (string, error) {
	if scope.guard {
		owned, err := q.TopicReviewGuard(ctx, db.TopicReviewGuardParams{AccountID: row.AccountID, WhatsappChat: scope.chat, JobID: row.ID})
		if err != nil {
			return "", err
		}
		if !owned {
			return "guard_missing", nil
		}
	}
	if scope.transition != nil {
		if _, err := transitionRoute(ctx, q, row, *scope.transition); errors.Is(err, delivery.ErrInvalid) {
			return "route_changed", nil
		} else if err != nil {
			return "", err
		}
	}
	if scope.native != nil {
		if err := nativeRepairRoute(ctx, q, row, *scope.native); errors.Is(err, relay.ErrNativeRepairUnsafe) || errors.Is(err, delivery.ErrLocalRetry) {
			return "route_or_history_conflict", nil
		} else if err != nil {
			return "", err
		}
		if err := nativeRepairUnsent(ctx, q, row, *scope.native, false); errors.Is(err, relay.ErrNativeRepairUnsafe) {
			return "source_already_delivered", nil
		} else if err != nil {
			return "", err
		}
	}
	if scope.kind == "inbox" {
		if _, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: scope.chat}); err == nil {
			return "route_changed", nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
	}
	if scope.kind != "status" {
		active, err := q.ActiveHistory(ctx, db.ActiveHistoryParams{AccountID: row.AccountID, WhatsappChat: scope.chat})
		if err != nil {
			return "", err
		}
		if active {
			return "history_active", nil
		}
	}
	effects, err := q.MetadataJobEffects(ctx, db.MetadataJobEffectsParams{AccountID: row.AccountID, JobID: row.ID})
	if err != nil {
		return "", err
	}
	for _, e := range effects {
		if e.EffectKey != scope.key && (e.State == "sending" || e.State == "uncertain") {
			return "other_uncertain_effect", nil
		}
	}
	if row.State == "running" || row.State == "pending" {
		return "job_active", nil
	}
	return "", nil
}

func inspectTopic(ctx context.Context, q *db.Queries, row db.Job) (recovery.TopicSnapshot, error) {
	identity, err := q.Identity(ctx)
	if err != nil {
		return recovery.TopicSnapshot{}, err
	}
	if identity.AccountID != row.AccountID {
		return recovery.TopicSnapshot{}, delivery.ErrInvalid
	}
	scope, err := topicScopeFor(ctx, q, row)
	if err != nil {
		return recovery.TopicSnapshot{}, err
	}
	effect, err := topicReviewEffect(ctx, q, row, scope)
	if err != nil {
		return recovery.TopicSnapshot{}, err
	}
	var request struct {
		ChatID int64  `json:"chat_id"`
		Name   string `json:"name"`
	}
	if effect.JobID != row.ID || json.Unmarshal(effect.Request, &request) != nil || request.ChatID >= 0 || request.Name == "" || conversation.TopicName(request.Name, "") != request.Name || scope.management != 0 && scope.management != request.ChatID || scope.name != "" && scope.name != request.Name {
		return recovery.TopicSnapshot{}, delivery.ErrInvalid
	}
	digest := sha256.Sum256(effect.Request)
	result := recovery.TopicSnapshot{AccountID: row.AccountID, Job: row.ID, Dedup: row.Dedup, Kind: row.Kind, State: row.State, Effect: scope.key, EffectState: effect.State, RequestDigest: hex.EncodeToString(digest[:]), Management: request.ChatID, Name: request.Name, Scope: scope.kind, SourceChat: scope.chat}
	result.Blocked, err = topicScopeBlocked(ctx, q, row, scope)
	return result, err
}

func topicReviewEffect(ctx context.Context, q *db.Queries, row db.Job, scope topicScope) (db.Effect, error) {
	effect, err := q.ReadTopicReviewEffect(ctx, db.ReadTopicReviewEffectParams{AccountID: row.AccountID, EffectKey: scope.key})
	if errors.Is(err, pgx.ErrNoRows) && scope.management < 0 && scope.name != "" {
		request, _ := json.Marshal(bot.CreateForumTopicParams{ChatID: scope.management, Name: scope.name})
		return db.Effect{AccountID: row.AccountID, JobID: row.ID, EffectKey: scope.key, State: "unprepared", Request: request}, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return effect, delivery.ErrInvalid
	}
	return effect, err
}

func (s *Store) InspectTopic(ctx context.Context, id int64) (recovery.TopicSnapshot, error) {
	if id <= 0 {
		return recovery.TopicSnapshot{}, delivery.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return recovery.TopicSnapshot{}, err
	}
	defer rollback(tx)
	q := s.queries.WithTx(tx)
	row, err := q.TopicReviewJob(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return recovery.TopicSnapshot{}, delivery.ErrInvalid
	}
	if err != nil {
		return recovery.TopicSnapshot{}, err
	}
	result, err := inspectTopic(ctx, q, row)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
