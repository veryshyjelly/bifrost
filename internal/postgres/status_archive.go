package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/relay"
	"github.com/veryshyjelly/bifrost/internal/statuses"
)

func statusOrigin(row db.Job, c statuses.Settings) error {
	if !statuses.ValidSettings(c) {
		return delivery.ErrInvalid
	}
	if row.Kind == "tg" {
		var envelope struct {
			Bot    string
			Update json.RawMessage
		}
		var u models.Update
		if json.Unmarshal(row.Payload, &envelope) != nil || json.Unmarshal(envelope.Update, &u) != nil || envelope.Bot != "master" || u.Message == nil || u.EditedMessage != nil || row.Dedup != "tg:master:"+strconv.FormatInt(u.ID, 10) || !ingress.AuthorizedUpdate(envelope.Update, &u, c.OwnerID) {
			return delivery.ErrInvalid
		}
		parsed, ok := control.ParseCommand(u.Message)
		if !ok || parsed.Name != "statuses" || u.Message.From == nil || u.Message.From.IsBot || u.Message.From.ID != c.OwnerID || u.Message.Chat.Type == models.ChatTypePrivate && u.Message.Chat.ID != c.OwnerID {
			return delivery.ErrInvalid
		}
		return nil
	}
	if row.Kind == "wa" {
		var h sdk.WebhookDelivery
		var account struct {
			AccountID string `json:"accountId"`
		}
		if json.Unmarshal(row.Payload, &h) != nil || json.Unmarshal(row.Payload, &account) != nil || h.Event != sdk.EventStatusReceived || h.SessionID != c.Session || h.IdempotencyKey == "" || row.Dedup != "ow:"+h.IdempotencyKey || row.Stream != "wa:"+statuses.ChatID || account.AccountID != row.AccountID {
			return delivery.ErrInvalid
		}
		return nil
	}
	var request struct {
		Session    string
		Management int64
	}
	if (row.Kind != "status-refresh" && row.Kind != "status-startup") || json.Unmarshal(row.Payload, &request) != nil || request.Session != c.Session || request.Management != c.Management || row.Stream != "status-control:"+strconv.FormatInt(c.Management, 10) {
		return delivery.ErrInvalid
	}
	return nil
}

func (s *Store) PrepareStatusTopic(ctx context.Context, j delivery.Job, c statuses.Settings) (statuses.Topic, error) {
	var result statuses.Topic
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := statusOrigin(row, c); err != nil {
			return err
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		feed, err := q.StatusFeed(ctx, db.StatusFeedParams{AccountID: row.AccountID, Management: c.Management})
		if err == nil {
			if json.Unmarshal(feed.Payload, &result) != nil || result.Feed.Management != c.Management {
				return delivery.ErrInvalid
			}
			result.Complete = feed.Complete
			if !feed.Complete && feed.JobID != row.ID {
				return delivery.ErrLocalRetry
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		result = statuses.Topic{Feed: statuses.Feed{Management: c.Management}, Key: row.Dedup + ":status-topic", Name: "WhatsApp Status"}
		raw, _ := json.Marshal(result)
		return q.CreateStatusFeed(ctx, db.CreateStatusFeedParams{AccountID: row.AccountID, Management: c.Management, JobID: row.ID, Payload: raw})
	})
	return result, err
}

func (s *Store) CommitStatusTopic(ctx context.Context, j delivery.Job, c statuses.Settings, thread int64) (statuses.Feed, error) {
	var result statuses.Feed
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := statusOrigin(row, c); err != nil {
			return err
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		feed, err := q.StatusFeed(ctx, db.StatusFeedParams{AccountID: row.AccountID, Management: c.Management})
		if err != nil {
			return err
		}
		var p statuses.Topic
		if json.Unmarshal(feed.Payload, &p) != nil || p.Feed.Management != c.Management {
			return delivery.ErrInvalid
		}
		if feed.Complete {
			if p.Feed.ThreadID != thread {
				return delivery.ErrInvalid
			}
			result = p.Feed
			return nil
		}
		if feed.JobID != row.ID || thread <= 1 {
			return delivery.ErrInvalid
		}
		effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: p.Key})
		if err != nil {
			return err
		}
		var topic models.ForumTopic
		if effect.JobID != row.ID || effect.State != "done" || json.Unmarshal(effect.Result, &topic) != nil || int64(topic.MessageThreadID) != thread {
			return delivery.ErrUncertain
		}
		reserved, err := q.ReservedTopic(ctx, db.ReservedTopicParams{AccountID: row.AccountID, TelegramChatID: c.Management, ThreadID: thread})
		if err != nil {
			return err
		}
		if reserved {
			return delivery.ErrInvalid
		}
		other, err := q.OtherTopicRoute(ctx, db.OtherTopicRouteParams{AccountID: row.AccountID, TelegramChatID: c.Management, ThreadID: thread, WhatsappChat: statuses.ChatID})
		if err != nil {
			return err
		}
		if other {
			return delivery.ErrInvalid
		}
		p.Feed.ThreadID, p.Complete = thread, true
		raw, _ := json.Marshal(p)
		if err := q.CompleteStatusFeed(ctx, db.CompleteStatusFeedParams{AccountID: row.AccountID, Management: c.Management, Payload: raw}); err != nil {
			return err
		}
		result = p.Feed
		return nil
	})
	return result, err
}

func statusMutes(ctx context.Context, q *db.Queries, account string, d *identity.Directory) (map[string]statuses.Mute, error) {
	rows, err := q.StatusMutes(ctx, account)
	if err != nil {
		return nil, err
	}
	result := map[string]statuses.Mute{}
	for _, raw := range rows {
		var m statuses.Mute
		if json.Unmarshal(raw, &m) != nil || !identity.IsPerson(m.Author) || m.Revision <= 0 {
			return nil, delivery.ErrInvalid
		}
		m.Author = d.Canonical(m.Author)
		if old, ok := result[m.Author]; !ok || m.Revision > old.Revision {
			result[m.Author] = m
		}
	}
	return result, nil
}

func (s *Store) FreezeStatusPost(ctx context.Context, j delivery.Job, c statuses.Settings) (statuses.Snapshot, error) {
	result := statuses.Snapshot{Skip: true}
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := statusOrigin(row, c); err != nil {
			return err
		}
		if row.Kind != "wa" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		var hook sdk.WebhookDelivery
		if json.Unmarshal(row.Payload, &hook) != nil {
			return delivery.ErrInvalid
		}
		var id struct {
			StatusID string `json:"statusId"`
		}
		if json.Unmarshal(hook.Data, &id) != nil || id.StatusID == "" {
			return nil
		}
		old, err := q.StatusPost(ctx, db.StatusPostParams{AccountID: row.AccountID, PostID: id.StatusID})
		if err == nil {
			if old.JobID != row.ID {
				return nil
			}
			if json.Unmarshal(old.Payload, &result.Post) != nil || json.Unmarshal(old.Source, &result.Source) != nil {
				return delivery.ErrInvalid
			}
			result.Skip, result.Complete = old.Skipped, old.Complete
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		post, ok := statuses.Decode(hook.Data, time.Now())
		if !ok {
			return nil
		}
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		mutes, err := statusMutes(ctx, q, row.AccountID, d)
		if err != nil {
			return err
		}
		canonical := d.Canonical(post.Author)
		result = statuses.Snapshot{Post: post, Source: post.Source(canonical), Skip: mutes[canonical].Active}
		if name := d.SenderName(canonical); name != "" && name != canonical {
			result.Source.Name = name
		}
		raw, _ := json.Marshal(post)
		source, _ := json.Marshal(result.Source)
		return q.CreateStatusPost(ctx, db.CreateStatusPostParams{AccountID: row.AccountID, PostID: post.ID, JobID: row.ID, Payload: raw, Source: source, Skipped: result.Skip})
	})
	return result, err
}

func (s *Store) CompleteStatusPost(ctx context.Context, j delivery.Job, id string) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		post, err := q.StatusPost(ctx, db.StatusPostParams{AccountID: row.AccountID, PostID: id})
		if err != nil {
			return err
		}
		if post.JobID != row.ID {
			return delivery.ErrInvalid
		}
		plan, err := q.NativeDeliveryPlan(ctx, db.NativeDeliveryPlanParams{AccountID: row.AccountID, PlanKey: relay.NativeIncomingKey(statuses.ChatID, id)})
		if err != nil {
			return err
		}
		if !plan.Complete || plan.JobID != row.ID {
			return delivery.ErrInvalid
		}
		return q.CompleteStatusPost(ctx, db.CompleteStatusPostParams{AccountID: row.AccountID, PostID: id, JobID: row.ID})
	})
}

func (s *Store) StatusBatch(ctx context.Context, j delivery.Job) (statuses.Batch, bool, error) {
	var result statuses.Batch
	found := false
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "status-refresh" && row.Kind != "status-startup" {
			return delivery.ErrInvalid
		}
		batch, err := q.StatusRefresh(ctx, db.StatusRefreshParams{AccountID: row.AccountID, JobID: row.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if json.Unmarshal(batch.Payload, &result.Posts) != nil {
			return delivery.ErrInvalid
		}
		result.Complete, found = batch.Complete, true
		return nil
	})
	return result, found, err
}
func (s *Store) FreezeStatusBatch(ctx context.Context, j delivery.Job, posts []statuses.Post) (statuses.Batch, error) {
	var result statuses.Batch
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "status-refresh" && row.Kind != "status-startup" {
			return delivery.ErrInvalid
		}
		batch, err := q.StatusRefresh(ctx, db.StatusRefreshParams{AccountID: row.AccountID, JobID: row.ID})
		if err == nil {
			if json.Unmarshal(batch.Payload, &result.Posts) != nil {
				return delivery.ErrInvalid
			}
			result.Complete = batch.Complete
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		for _, p := range posts {
			if p.ID == "" || !identity.IsPerson(p.Author) || p.PostedAt <= 0 || p.ExpiresAt <= 0 {
				return delivery.ErrInvalid
			}
		}
		raw, _ := json.Marshal(posts)
		if err := q.CreateStatusRefresh(ctx, db.CreateStatusRefreshParams{AccountID: row.AccountID, JobID: row.ID, Payload: raw}); err != nil {
			return err
		}
		result.Posts = posts
		return nil
	})
	return result, err
}
func (s *Store) EnqueueStatusBatch(ctx context.Context, j delivery.Job, c statuses.Settings) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := statusOrigin(row, c); err != nil {
			return err
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		mutes, err := statusMutes(ctx, q, row.AccountID, d)
		if err != nil {
			return err
		}
		batch, err := q.StatusRefresh(ctx, db.StatusRefreshParams{AccountID: row.AccountID, JobID: row.ID})
		if err != nil {
			return err
		}
		if batch.Complete {
			return nil
		}
		var posts []statuses.Post
		if json.Unmarshal(batch.Payload, &posts) != nil {
			return delivery.ErrInvalid
		}
		for _, p := range posts {
			if _, err := q.StatusPost(ctx, db.StatusPostParams{AccountID: row.AccountID, PostID: p.ID}); err == nil {
				continue
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if mutes[d.Canonical(p.Author)].Active {
				raw, _ := json.Marshal(p)
				source, _ := json.Marshal(p.Source(d.Canonical(p.Author)))
				if err := q.CreateStatusPost(ctx, db.CreateStatusPostParams{AccountID: row.AccountID, PostID: p.ID, JobID: row.ID, Payload: raw, Source: source, Skipped: true}); err != nil {
					return err
				}
				continue
			}
			key := "status-post:" + strconv.FormatInt(c.Management, 10) + ":" + p.ID
			data := map[string]any{"statusId": p.ID, "contact": sdk.StatusContact{ID: p.Author, Name: p.Name}, "type": p.Kind, "caption": p.Body, "postedAt": p.PostedAt, "expiresAt": p.ExpiresAt, "hasMedia": p.HasMedia, "mediaOmitted": p.MediaOmitted}
			raw, _ := json.Marshal(map[string]any{"accountId": row.AccountID, "sessionId": c.Session, "event": sdk.EventStatusReceived, "idempotencyKey": key, "data": data})
			_, err := q.Enqueue(ctx, db.EnqueueParams{AccountID: row.AccountID, Dedup: "ow:" + key, Stream: "wa:" + statuses.ChatID, Kind: "wa", Payload: raw})
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		return q.CompleteStatusRefresh(ctx, db.CompleteStatusRefreshParams{AccountID: row.AccountID, JobID: row.ID})
	})
}

func prepareNativeStatusScope(ctx context.Context, q *db.Queries, row db.Job, p *relay.NativePlan) error {
	if row.Kind != "wa" || row.Stream != "wa:"+statuses.ChatID || p.WhatsAppChat != statuses.ChatID || p.BotID != "master" || p.Key != relay.NativeIncomingKey(statuses.ChatID, p.WhatsAppMessage) || !p.Presentation.Status {
		return delivery.ErrInvalid
	}
	post, err := q.StatusPost(ctx, db.StatusPostParams{AccountID: row.AccountID, PostID: p.WhatsAppMessage})
	if err != nil {
		return err
	}
	var source identity.Source
	var frozen statuses.Post
	if post.JobID != row.ID || post.Skipped || json.Unmarshal(post.Source, &source) != nil || json.Unmarshal(post.Payload, &frozen) != nil || !reflect.DeepEqual(source, p.Source) || !reflect.DeepEqual(frozen.Message(), p.Message) {
		return delivery.ErrInvalid
	}
	var envelope sdk.WebhookDelivery
	if json.Unmarshal(row.Payload, &envelope) != nil || envelope.SessionID != p.Session || envelope.Event != sdk.EventStatusReceived {
		return delivery.ErrInvalid
	}
	feed, err := q.StatusFeed(ctx, db.StatusFeedParams{AccountID: row.AccountID, Management: p.TelegramChat})
	if err != nil {
		return err
	}
	var topic statuses.Topic
	if !feed.Complete || json.Unmarshal(feed.Payload, &topic) != nil || topic.Feed.ThreadID != p.ThreadID {
		return delivery.ErrInvalid
	}
	reserved, err := q.ReservedTopic(ctx, db.ReservedTopicParams{AccountID: row.AccountID, TelegramChatID: p.TelegramChat, ThreadID: p.ThreadID})
	if err != nil {
		return err
	}
	if reserved {
		return delivery.ErrLocalRetry
	}
	return nil
}

var _ statuses.Store = (*Store)(nil)

// QueueStatusStartup is inert until explicit local API migration has completed.
// Callback registration remains a separate, explicitly composed capability.
func (s *Store) QueueStatusStartup(ctx context.Context, account, key string, c statuses.Settings) (bool, error) {
	if !statuses.ValidSettings(c) || account == "" || key == "" {
		return false, delivery.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer rollback(tx)
	q := s.queries.WithTx(tx)
	if err := q.LockContactDirectory(ctx, account); err != nil {
		return false, err
	}
	identity, err := q.Identity(ctx)
	if err != nil {
		return false, err
	}
	if identity.AccountID != account {
		return false, delivery.ErrInvalid
	}
	if _, err := q.BotLifecycle(ctx, db.BotLifecycleParams{AccountID: account, BotID: "master"}); errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	_, err = metadataEnqueue(ctx, q, account, key, "status-control:"+strconv.FormatInt(c.Management, 10), "status-startup", map[string]any{"Session": c.Session, "Management": c.Management})
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
