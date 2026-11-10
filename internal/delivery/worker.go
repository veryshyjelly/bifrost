package delivery

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
)

type WorkerStore interface {
	EffectStore
	Claim(context.Context, time.Duration) (Job, error)
	Renew(context.Context, Job, time.Duration) error
	SetJobState(context.Context, Job, State, time.Duration) error
	RecoverExpired(context.Context) (int, error)
}

type Handler func(context.Context, Job, *Effects) error

// Report contains only safe execution metadata, never payloads, tokens or errors.
type Report struct {
	JobID  int64
	State  State
	Reason string
	Delay  time.Duration
}

type WorkerOptions struct {
	Concurrency                                                 int
	Lease, RenewEvery, PollEvery, StoreTimeout, ShutdownTimeout time.Duration
	MaxAttempts                                                 int32
	RetryBase, RetryMax                                         time.Duration
	Report                                                      func(Report)
}

type Worker struct {
	store    WorkerStore
	handlers map[string]Handler
	c        WorkerOptions
}

func NewWorker(store WorkerStore, handlers map[string]Handler, c WorkerOptions) (*Worker, error) {
	if c.Concurrency == 0 {
		c.Concurrency = 1
	}
	if c.Lease == 0 {
		c.Lease = time.Minute
	}
	if c.RenewEvery == 0 {
		c.RenewEvery = c.Lease / 3
	}
	if c.PollEvery == 0 {
		c.PollEvery = time.Second
	}
	if c.StoreTimeout == 0 {
		c.StoreTimeout = min(c.Lease/3, 10*time.Second)
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 10 * time.Second
	}
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 8
	}
	if c.RetryBase == 0 {
		c.RetryBase = time.Second
	}
	if c.RetryMax == 0 {
		c.RetryMax = 5 * time.Minute
	}
	if store == nil || len(handlers) == 0 || c.Concurrency < 1 || c.Concurrency > 64 || c.Lease <= 0 || c.Lease > time.Duration(math.MaxInt32)*time.Second || c.RenewEvery <= 0 || c.RenewEvery >= c.Lease/2 || c.StoreTimeout <= 0 || c.StoreTimeout > c.Lease/3 || c.PollEvery <= 0 || c.ShutdownTimeout <= 0 || c.MaxAttempts < 1 || c.RetryBase <= 0 || c.RetryMax < c.RetryBase || c.RetryMax > time.Duration(math.MaxInt32)*time.Second {
		return nil, ErrInvalid
	}
	copyHandlers := make(map[string]Handler, len(handlers))
	for kind, h := range handlers {
		if kind == "" || h == nil {
			return nil, ErrInvalid
		}
		copyHandlers[kind] = h
	}
	return &Worker{store: store, handlers: copyHandlers, c: c}, nil
}

type lease struct {
	mu     sync.Mutex
	until  time.Time
	lost   bool
	cancel context.CancelFunc
}

func (l *lease) valid() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return !l.lost && time.Now().Before(l.until)
}
func (l *lease) renew(start time.Time, d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.lost {
		l.until = start.Add(d)
	}
}
func (l *lease) lose() { l.mu.Lock(); l.lost = true; l.mu.Unlock(); l.cancel() }

func (w *Worker) notice(r Report) {
	if w.c.Report != nil {
		w.c.Report(r)
	}
}

// Run stops claiming on cancellation or persistence failure. It never resets a
// live claim at startup, and it bounds waiting for a handler that ignores cancel.
func (w *Worker) Run(ctx context.Context) error {
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	completed := make(chan struct{}, w.c.Concurrency)
	active := 0
	ticker := time.NewTicker(w.c.PollEvery)
	defer ticker.Stop()
	nextRecovery := time.Time{}
	var result error
loop:
	for {
		if workCtx.Err() != nil {
			break
		}
		if !time.Now().Before(nextRecovery) {
			call, done := context.WithTimeout(workCtx, w.c.StoreTimeout)
			_, err := w.store.RecoverExpired(call)
			done()
			if err != nil {
				if workCtx.Err() == nil {
					result = ErrPersistence
				}
				break
			}
			nextRecovery = time.Now().Add(w.c.Lease / 3)
		}
		for active < w.c.Concurrency {
			start := time.Now()
			call, done := context.WithTimeout(workCtx, w.c.StoreTimeout)
			job, err := w.store.Claim(call, w.c.Lease)
			done()
			if errors.Is(err, ErrNoJob) {
				break
			}
			if err != nil {
				if workCtx.Err() == nil {
					result = ErrPersistence
				}
				break loop
			}
			active++
			wg.Go(func() { defer func() { completed <- struct{}{} }(); w.execute(workCtx, job, start) })
		}
		select {
		case <-workCtx.Done():
			break loop
		case <-completed:
			active--
		case <-ticker.C:
		}
	}
	cancel()
	stopped := make(chan struct{})
	go func() { wg.Wait(); close(stopped) }()
	select {
	case <-stopped:
		return result
	case <-time.After(w.c.ShutdownTimeout):
		return errors.New("worker shutdown timed out; unfinished claims require lease recovery")
	}
}

func (w *Worker) heartbeat(ctx context.Context, job Job, fence *lease, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(w.c.RenewEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			start := time.Now()
			call, cancel := context.WithTimeout(ctx, w.c.StoreTimeout)
			err := w.store.Renew(call, job, w.c.Lease)
			cancel()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				fence.lose()
				return
			}
			fence.renew(start, w.c.Lease)
		}
	}
}

func (w *Worker) execute(parent context.Context, job Job, start time.Time) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	fence := &lease{until: start.Add(w.c.Lease), cancel: cancel}
	hbCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	go w.heartbeat(hbCtx, job, fence, heartbeatDone)
	effects := &Effects{ctx: ctx, job: job, store: w.store, fence: fence, timeout: w.c.StoreTimeout, refused: map[string]bool{}}
	handler := w.handlers[job.Kind]
	err := ErrUnsupported
	if ctx.Err() != nil {
		err = ctx.Err()
	} else if handler != nil {
		err = handle(ctx, job, effects, handler)
	}
	effects.mu.Lock()
	if err == nil && effects.stopped {
		err = ErrUncertain
	}
	effects.mu.Unlock()
	stopHeartbeat()
	<-heartbeatDone
	if !fence.valid() || errors.Is(err, ErrClaimLost) {
		w.notice(Report{JobID: job.ID, Reason: "claim_lost"})
		return
	}
	state, delay := w.next(job, err)
	if parent.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		// The store still rejects pending when a sending/uncertain effect exists.
		state, delay = Pending, 0
	}
	call, done := context.WithTimeout(context.WithoutCancel(parent), w.c.StoreTimeout)
	saveErr := w.store.SetJobState(call, job, state, delay)
	done()
	if errors.Is(saveErr, ErrInFlight) || errors.Is(saveErr, ErrUncertain) {
		state = Uncertain
		saveErr = w.setState(parent, job, state, 0)
	} else if errors.Is(saveErr, ErrIncomplete) {
		state = Failed
		saveErr = w.setState(parent, job, state, 0)
	}
	if saveErr != nil {
		w.notice(Report{JobID: job.ID, Reason: "state_not_saved"})
		return
	}
	reason := "completed"
	if state == Pending {
		reason = "retry_scheduled"
	} else if state == Uncertain {
		reason = "review_required"
	} else if state == Failed {
		reason = "definitive_failure"
		if errors.Is(err, ErrUnsupported) {
			reason = "unsupported_kind"
		}
	}
	w.notice(Report{JobID: job.ID, State: state, Delay: delay, Reason: reason})
}

func (w *Worker) setState(parent context.Context, job Job, state State, delay time.Duration) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), w.c.StoreTimeout)
	defer cancel()
	return w.store.SetJobState(ctx, job, state, delay)
}
func handle(ctx context.Context, job Job, e *Effects, h Handler) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrUncertain
		}
	}()
	return h(ctx, job, e)
}

func (w *Worker) next(job Job, err error) (State, time.Duration) {
	if err == nil {
		return Done, 0
	}
	retry, unknown, requested := decision(err)
	if unknown {
		return Uncertain, 0
	}
	if !retry || job.Attempts >= w.c.MaxAttempts || requested > time.Duration(math.MaxInt32)*time.Second {
		return Failed, 0
	}
	delay := w.c.RetryBase
	for i := int32(0); i < job.Attempts && delay < w.c.RetryMax; i++ {
		if delay > w.c.RetryMax/2 {
			delay = w.c.RetryMax
		} else {
			delay *= 2
		}
	}
	return Pending, max(delay, requested)
}
