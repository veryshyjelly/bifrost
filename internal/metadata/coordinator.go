package metadata

import (
	"context"
	"errors"
	"time"

	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
	"github.com/veryshyjelly/bifrost/internal/schedule"
)

type RunOptions struct {
	Session                string
	OwnerID, Management    int64
	Cron, Timezone         string
	CommandTTL, ControlTTL int
}
type Outcome struct{ Warnings []string }
type RunStore interface {
	AdmitMetadataRun(context.Context, delivery.Job, RunOptions) (string, error)
	StartMetadataRun(context.Context, delivery.Job, RunOptions) error
	BeginMetadataTarget(context.Context, delivery.Job, RunOptions) (bool, error)
	DiscoverMetadataContacts(context.Context, delivery.Job, RunOptions) error
	CompleteMetadataTarget(context.Context, delivery.Job, Outcome, RunOptions) error
	SealMetadataRun(context.Context, delivery.Job, RunOptions) error
	ScheduleMetadataRun(context.Context, string, RunOptions, time.Time) (string, bool, error)
}
type Coordinator struct {
	store    RunStore
	text     *Service
	pictures *Pictures
	advice   *Inspector
	options  RunOptions
	cron     *schedule.Cron
}

func NewCoordinator(store RunStore, text *Service, pictures *Pictures, advice *Inspector, c RunOptions) (*Coordinator, error) {
	if c.CommandTTL == 0 {
		c.CommandTTL = 60
	}
	if c.ControlTTL == 0 {
		c.ControlTTL = 600
	}
	if store == nil || text == nil || pictures == nil || advice == nil || c.Session == "" || c.OwnerID <= 0 || c.Management >= 0 || c.CommandTTL < 5 || c.CommandTTL > 86400 || c.ControlTTL < 5 || c.ControlTTL > 86400 {
		return nil, delivery.ErrInvalid
	}
	cron, err := schedule.New(c.Cron, c.Timezone)
	if err != nil {
		return nil, err
	}
	c.Cron = cron.Expression()
	c.Timezone = cron.Timezone()
	return &Coordinator{store: store, text: text, pictures: pictures, advice: advice, options: c, cron: cron}, nil
}
func (c *Coordinator) Admit(ctx context.Context, j delivery.Job, _ *delivery.Effects) error {
	_, err := c.store.AdmitMetadataRun(ctx, j, c.options)
	return local(err)
}
func (c *Coordinator) Start(ctx context.Context, j delivery.Job, _ *delivery.Effects) error {
	return runLocal(c.store.StartMetadataRun(ctx, j, c.options))
}
func (c *Coordinator) Seal(ctx context.Context, j delivery.Job, _ *delivery.Effects) error {
	return runLocal(c.store.SealMetadataRun(ctx, j, c.options))
}
func runLocal(err error) error {
	if errors.Is(err, delivery.ErrDeferred) {
		return err
	}
	return local(err)
}
func (c *Coordinator) Handlers() map[string]delivery.Handler {
	return map[string]delivery.Handler{"sync-start": c.Start, "sync-target": c.Target, "sync-seal": c.Seal}
}
func (c *Coordinator) Tick(ctx context.Context, account string, now time.Time, configured bool) (string, bool, error) {
	if !configured || !c.cron.Matches(now) {
		return "", false, nil
	}
	run, created, err := c.store.ScheduleMetadataRun(ctx, account, c.options, now)
	return run, created, local(err)
}

// Schedule is started only by a fully configured runtime composition. Persisted
// minutes handle restarts/multiple schedulers; missed minutes are not replayed.
func (c *Coordinator) Schedule(ctx context.Context, account string, configured func() bool) error {
	if configured == nil {
		return delivery.ErrInvalid
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		now := time.Now()
		if _, _, err := c.Tick(ctx, account, now, configured()); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		timer := time.NewTimer(time.Until(now.Truncate(time.Minute).Add(time.Minute)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (c *Coordinator) Target(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	complete, err := c.store.BeginMetadataTarget(ctx, j, c.options)
	if err != nil {
		return runLocal(err)
	}
	if complete {
		return nil
	}
	warnings := []string{}
	finish := func(err error, partial *Inspection) error {
		if err != nil {
			if errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, delivery.ErrLocalRetry) || errors.Is(err, delivery.ErrPersistence) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			if errors.Is(err, delivery.ErrUncertain) || errors.Is(err, delivery.ErrInFlight) {
				return err
			}
			var outcome delivery.Outcome
			if errors.As(err, &outcome) {
				retry, uncertain, _ := outcome.DeliveryOutcome()
				if uncertain || retry && j.Attempts < 4 {
					return err
				}
			} else if !errors.Is(err, delivery.ErrInvalid) && !errors.Is(err, delivery.ErrUnsupported) {
				return err
			}
			warnings = append(warnings, err.Error())
			if partial != nil && partial.JobID == j.ID {
				settings := control.SyncSettings{Session: c.options.Session, OwnerID: c.options.OwnerID, Management: c.options.Management, CommandTTL: c.options.CommandTTL, ControlTTL: c.options.ControlTTL}
				_, saveErr := c.advice.store.PrepareMetadataInspection(ctx, j, *partial, settings)
				if saveErr != nil && !errors.Is(saveErr, ErrTarget) && !errors.Is(saveErr, delivery.ErrInvalid) {
					return local(saveErr)
				}
			}
		}
		return local(c.store.CompleteMetadataTarget(ctx, j, Outcome{Warnings: warnings}, c.options))
	}
	if _, err := c.text.Prepare(ctx, j); err != nil {
		return finish(err, nil)
	}
	if err := c.store.DiscoverMetadataContacts(ctx, j, c.options); err != nil {
		return local(err)
	}
	if err := c.text.ApplyTextManaged(ctx, j, e); err != nil {
		return finish(err, nil)
	}
	p, _, err := c.text.store.MetadataPlan(ctx, j)
	if err != nil {
		return local(err)
	}
	for i := range p.Changes {
		if warning := p.TextRefusals[i]; warning != "" {
			warnings = append(warnings, warning)
		}
	}
	if _, err := c.pictures.Prepare(ctx, j); err != nil {
		return finish(err, nil)
	}
	if err := c.pictures.Apply(ctx, j, e); err != nil {
		return finish(err, nil)
	}
	inspection, err := c.advice.Inspect(ctx, j)
	if err != nil {
		return finish(err, &inspection)
	}
	return finish(nil, nil)
}

func definiteMetadataRefusal(err error) bool {
	var provider *providers.Error
	if !errors.As(err, &provider) {
		return false
	}
	return !provider.Retry && !provider.Uncertain
}
