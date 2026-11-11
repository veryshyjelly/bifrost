package conversation

import (
	"context"
	"errors"
	"time"

	"github.com/go-telegram/bot"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

// Retirement freezes the former inboxes independently of conversation delivery.
// A reserved topic cannot become a delivery target again, even after a lost
// delete response. Completed message-send receipts remain in the effect ledger.
type Retirement struct {
	ChatID     string
	Management int64
	// CurrentThread is -1 when the snapshot route was outside management.
	CurrentThread int64
	Topics        []RetiringTopic
	Complete      bool
}

type RetiringTopic struct {
	ThreadID int64
	Deleted  bool
}

type sharedInbox struct{}

func (sharedInbox) Error() string                                { return "management topic is shared with another conversation" }
func (sharedInbox) DeliveryOutcome() (bool, bool, time.Duration) { return false, false, 0 }

var ErrSharedInbox error = sharedInbox{}

type RetirementStore interface {
	PrepareInboxRetirement(context.Context, delivery.Job, int64) (Retirement, error)
	FinishTopicRetirement(context.Context, delivery.Job, int64) error
	CompleteInboxRetirement(context.Context, delivery.Job) error
}

type TopicDeleter interface {
	DeleteForumTopic(context.Context, *bot.DeleteForumTopicParams) error
}

type Retirer struct {
	store      RetirementStore
	master     TopicDeleter
	management int64
}

func NewRetirer(store RetirementStore, master TopicDeleter, management int64) (*Retirer, error) {
	if store == nil || master == nil || management >= 0 {
		return nil, delivery.ErrInvalid
	}
	return &Retirer{store: store, master: master, management: management}, nil
}

func retirementError(err error) error {
	if err == nil || errors.Is(err, delivery.ErrInvalid) || errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, ErrSharedInbox) {
		return err
	}
	return delivery.ErrLocalRetry
}

func (r *Retirer) Retire(ctx context.Context, job delivery.Job, e *delivery.Effects) error {
	if e == nil || job.Kind != "control-retire-inbox" {
		return delivery.ErrInvalid
	}
	plan, err := r.store.PrepareInboxRetirement(ctx, job, r.management)
	if err != nil {
		return retirementError(err)
	}
	if plan.Complete {
		return nil
	}
	for _, topic := range plan.Topics {
		if topic.Deleted {
			continue
		}
		if err := e.Permit(); err != nil {
			return err
		}
		// Like notice deletion, this operation is idempotent and has its own
		// durable target/outcome journal. It cannot make message sends uncertain.
		if err := r.master.DeleteForumTopic(ctx, &bot.DeleteForumTopicParams{ChatID: plan.Management, MessageThreadID: int(topic.ThreadID)}); err != nil {
			return err
		}
		if err := r.store.FinishTopicRetirement(ctx, job, topic.ThreadID); err != nil {
			return retirementError(err)
		}
	}
	return retirementError(r.store.CompleteInboxRetirement(ctx, job))
}
