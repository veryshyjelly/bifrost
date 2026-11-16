package statuses

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

type Topic struct {
	Feed      Feed
	Key, Name string
	Complete  bool
}

// Snapshot freezes a post's normalized author and skip/completion decision before
// archive delivery. Retrying a skipped snapshot does not reconsider eligibility.
type Snapshot struct {
	Post           Post
	Source         identity.Source
	Skip, Complete bool
}

// Batch is the saved ordered refresh result, reused instead of listing live
// Status again when the refresh job resumes.
type Batch struct {
	Posts    []Post
	Complete bool
}

// Store freezes Status topic, post and batch state under worker claims. Delivery
// completion is distinct from admitting the refresh batch's individual posts.
type Store interface {
	PrepareStatusTopic(context.Context, delivery.Job, Settings) (Topic, error)
	CommitStatusTopic(context.Context, delivery.Job, Settings, int64) (Feed, error)
	FreezeStatusPost(context.Context, delivery.Job, Settings) (Snapshot, error)
	StatusDeliveryTarget(context.Context, string, string) (Feed, bool, error)
	CompleteStatusPost(context.Context, delivery.Job, string) error
	StatusBatch(context.Context, delivery.Job) (Batch, bool, error)
	FreezeStatusBatch(context.Context, delivery.Job, []Post) (Batch, error)
	EnqueueStatusBatch(context.Context, delivery.Job, Settings) error
}
type WhatsApp interface {
	ListStatuses(context.Context, string) (*sdk.StatusListResponse, error)
}
type Telegram interface {
	CreateForumTopic(context.Context, *bot.CreateForumTopicParams) (*models.ForumTopic, error)
}
type Archive interface {
	DeliverStatus(context.Context, delivery.Job, *delivery.Effects, Feed, Snapshot) error
}
type Service struct {
	store   Store
	wa      WhatsApp
	tg      Telegram
	archive Archive
	c       Settings
}

func New(store Store, wa WhatsApp, tg Telegram, archive Archive, c Settings) (*Service, error) {
	if c.CommandTTL == 0 {
		c.CommandTTL = 60
	}
	if c.NoticeTTL == 0 {
		c.NoticeTTL = 60
	}
	if c.ControlTTL == 0 {
		c.ControlTTL = 600
	}
	if store == nil || wa == nil || tg == nil || archive == nil || !ValidSettings(c) {
		return nil, delivery.ErrInvalid
	}
	return &Service{store: store, wa: wa, tg: tg, archive: archive, c: c}, nil
}
func ValidSettings(c Settings) bool {
	return c.Session != "" && c.OwnerID > 0 && c.Management < 0 && c.CommandTTL >= 5 && c.CommandTTL <= 86400 && c.NoticeTTL >= 5 && c.NoticeTTL <= 86400 && c.ControlTTL >= 5 && c.ControlTTL <= 86400
}
func local(err error) error {
	if err == nil || errors.Is(err, delivery.ErrInvalid) || errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, delivery.ErrUncertain) || errors.Is(err, delivery.ErrUnsupported) || errors.Is(err, delivery.ErrDeferred) {
		return err
	}
	var outcome delivery.Outcome
	if errors.As(err, &outcome) {
		return err
	}
	return delivery.ErrLocalRetry
}
func (s *Service) topic(ctx context.Context, j delivery.Job, e *delivery.Effects) (Feed, error) {
	p, err := s.store.PrepareStatusTopic(ctx, j, s.c)
	if err != nil {
		return Feed{}, local(err)
	}
	if p.Complete {
		return p.Feed, nil
	}
	request, _ := json.Marshal(bot.CreateForumTopicParams{ChatID: p.Feed.Management, Name: p.Name})
	receipt, err := e.Do(p.Key, request, func(call context.Context, raw json.RawMessage) (json.RawMessage, error) {
		var request bot.CreateForumTopicParams
		if json.Unmarshal(raw, &request) != nil {
			return nil, delivery.ErrInvalid
		}
		r, err := s.tg.CreateForumTopic(call, &request)
		if err != nil {
			return nil, err
		}
		if r == nil || r.MessageThreadID <= 1 {
			return nil, providers.InvalidResponse("telegram", true)
		}
		return json.Marshal(r)
	})
	if err != nil {
		return Feed{}, err
	}
	var topic models.ForumTopic
	if json.Unmarshal(receipt, &topic) != nil || topic.MessageThreadID <= 1 {
		return Feed{}, delivery.ErrUncertain
	}
	feed, err := s.store.CommitStatusTopic(ctx, j, s.c, int64(topic.MessageThreadID))
	return feed, local(err)
}
func (s *Service) Receive(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "wa" || e == nil {
		return delivery.ErrInvalid
	}
	p, err := s.store.FreezeStatusPost(ctx, j, s.c)
	if err != nil {
		return local(err)
	}
	if p.Skip || p.Complete {
		return nil
	}
	feed, prepared, err := s.store.StatusDeliveryTarget(ctx, j.AccountID, p.Post.ID)
	if err != nil {
		return local(err)
	}
	if !prepared {
		feed, err = s.topic(ctx, j, e)
	}
	if err != nil {
		return err
	}
	if err := s.archive.DeliverStatus(ctx, j, e, feed, p); err != nil {
		return err
	}
	return local(s.store.CompleteStatusPost(ctx, j, p.Post.ID))
}

// Refresh freezes the provider list once, then resumes durable batch admission.
// Listing Status is passive and does not send read or view receipts.
func (s *Service) Refresh(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if (j.Kind != "status-refresh" && j.Kind != "status-startup") || e == nil {
		return delivery.ErrInvalid
	}
	if _, err := s.topic(ctx, j, e); err != nil {
		return err
	}
	batch, found, err := s.store.StatusBatch(ctx, j)
	if err != nil {
		return local(err)
	}
	if batch.Complete {
		return nil
	}
	if !found {
		if err := e.Permit(); err != nil {
			return err
		}
		r, err := s.wa.ListStatuses(ctx, s.c.Session)
		if err != nil {
			return err
		}
		if r == nil || r.Statuses == nil {
			return providers.InvalidResponse("openwa", false)
		}
		posts := make([]Post, 0, len(r.Statuses))
		now := time.Now()
		for _, r := range r.Statuses {
			if p, ok := FromRecord(r, now); ok {
				posts = append(posts, p)
			}
		}
		sort.SliceStable(posts, func(i, j int) bool { return posts[i].PostedAt < posts[j].PostedAt })
		if _, err := s.store.FreezeStatusBatch(ctx, j, posts); err != nil {
			return local(err)
		}
	}
	return local(s.store.EnqueueStatusBatch(ctx, j, s.c))
}
func (s *Service) Handlers() map[string]delivery.Handler {
	return map[string]delivery.Handler{"status-refresh": s.Refresh, "status-startup": s.Refresh}
}
