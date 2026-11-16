// Package polls freezes authorized source-poll selections and delivers votes
// independently of callback acknowledgements and ordinary message sends.
package polls

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

type Settings struct {
	Session   string
	OwnerID   int64
	NoticeTTL int
}

// Vote freezes the authorized selection and original poll action for a durable vote job.
type Vote struct {
	Key, Session, State string
	NoticeTTL           int
	Action              relay.PollAction
	Request             sdk.VotePollRequest
}

// Frozen returns the stable effect key and request bytes for this admitted vote.
func (v Vote) Frozen() (string, json.RawMessage, error) {
	if v.Key == "" || v.Session == "" || v.Request.ChatID == "" || v.Request.PollMessageID == "" {
		return "", nil, delivery.ErrInvalid
	}
	request, err := json.Marshal(struct {
		Session string
		Request sdk.VotePollRequest
	}{v.Session, v.Request})
	return v.Key + ":vote", request, err
}

// Store separates callback admission, vote delivery and source vote observations.
type Store interface {
	PreparePollCallback(context.Context, delivery.Job, Settings) (control.Plan, error)
	PreparePollVote(context.Context, delivery.Job, Settings) (Vote, error)
	CompletePollVote(context.Context, delivery.Job) error
	RefusePollVote(context.Context, delivery.Job, int) error
	NormalizeChange(context.Context, delivery.Job, string) error
	ObservePollVote(context.Context, delivery.Job, Settings) error
}
type WhatsApp interface {
	VotePoll(context.Context, string, sdk.VotePollRequest) error
}
type Service struct {
	store   Store
	wa      WhatsApp
	replies *control.Service
	c       Settings
}

func New(store Store, wa WhatsApp, replies *control.Service, c Settings) (*Service, error) {
	if c.NoticeTTL == 0 {
		c.NoticeTTL = 60
	}
	if store == nil || wa == nil || replies == nil || c.Session == "" || c.OwnerID <= 0 || c.NoticeTTL < 5 || c.NoticeTTL > 86400 {
		return nil, delivery.ErrInvalid
	}
	return &Service{store: store, wa: wa, replies: replies, c: c}, nil
}
func (s *Service) Callback(ctx context.Context, j delivery.Job, e *delivery.Effects, _ string, u *models.Update) error {
	if j.Kind != "tg" || u == nil || e == nil {
		return delivery.ErrInvalid
	}
	if u.CallbackQuery == nil || !strings.HasPrefix(u.CallbackQuery.Data, "vote:") {
		return delivery.ErrUnsupported
	}
	if u.CallbackQuery.From.IsBot || u.CallbackQuery.From.ID != s.c.OwnerID {
		return nil
	}
	p, err := s.store.PreparePollCallback(ctx, j, s.c)
	if err != nil {
		return local(err)
	}
	return s.replies.Send(ctx, j, e, p)
}
func (s *Service) Vote(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "control-poll-vote" || e == nil {
		return delivery.ErrInvalid
	}
	p, err := s.store.PreparePollVote(ctx, j, s.c)
	if err != nil {
		return local(err)
	}
	if p.State == "done" || p.State == "refused" {
		return nil
	}
	_, request, err := p.Frozen()
	if err != nil {
		return err
	}
	// Like command-menu settings, voting sets complete provider state. Keep a
	// frozen plan and fenced completion rather than an ambiguous-send effect:
	// after a crash the same selection can safely be applied again.
	var payload struct {
		Session string
		Request sdk.VotePollRequest
	}
	if json.Unmarshal(request, &payload) != nil || payload.Session != p.Session {
		return delivery.ErrInvalid
	}
	if err := e.Permit(); err != nil {
		return err
	}
	err = s.wa.VotePoll(ctx, payload.Session, payload.Request)
	if err != nil {
		var refusal *providers.Error
		if !errors.As(err, &refusal) || refusal.Retry || refusal.Uncertain {
			return err
		}
		switch refusal.Status {
		case 400, 403, 404, 501:
		default:
			return err
		}
		return local(s.store.RefusePollVote(ctx, j, refusal.Status))
	}
	return local(s.store.CompletePollVote(ctx, j))
}
func (s *Service) Observe(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "wa" || e == nil {
		return delivery.ErrInvalid
	}
	var hook sdk.WebhookDelivery
	if json.Unmarshal(j.Payload, &hook) != nil {
		return delivery.ErrInvalid
	}
	if hook.Event != sdk.EventPollVote {
		return delivery.ErrUnsupported
	}
	if err := s.store.NormalizeChange(ctx, j, s.c.Session); err != nil {
		return local(err)
	}
	return local(s.store.ObservePollVote(ctx, j, s.c))
}
func local(err error) error {
	if err == nil || errors.Is(err, delivery.ErrInvalid) || errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, delivery.ErrUncertain) || errors.Is(err, delivery.ErrDeferred) || errors.Is(err, delivery.ErrUnsupported) {
		return err
	}
	return delivery.ErrLocalRetry
}
