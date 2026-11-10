package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/veryshyjelly/bifrost/internal/providers"
)

type workerFixture struct {
	mu         sync.Mutex
	claimed    bool
	job        Job
	effects    map[string]Effect
	state      State
	delay      time.Duration
	renewErr   error
	renewed    chan struct{}
	renewOnce  sync.Once
	resolveErr error
	beginErr   error
}

func fixtureWorkerStore() *workerFixture {
	return &workerFixture{job: Job{ID: 1, Token: "private-claim-token", Attempts: 1, Event: Event{AccountID: "fixture", Kind: "fixture", Stream: "fixture", Dedup: "fixture", Payload: json.RawMessage(`{}`)}}, effects: map[string]Effect{}}
}
func (f *workerFixture) Claim(context.Context, time.Duration) (Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimed {
		return Job{}, ErrNoJob
	}
	f.claimed = true
	return f.job, nil
}
func (f *workerFixture) Renew(context.Context, Job, time.Duration) error {
	if f.renewed != nil {
		f.renewOnce.Do(func() { close(f.renewed) })
	}
	return f.renewErr
}
func (f *workerFixture) RecoverExpired(context.Context) (int, error) { return 0, nil }
func (f *workerFixture) BeginEffect(_ context.Context, _ Job, key string, request json.RawMessage) (Effect, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beginErr != nil {
		return Effect{}, f.beginErr
	}
	e, ok := f.effects[key]
	if !ok {
		e = Effect{State: "pending", Request: append(json.RawMessage(nil), request...)}
	}
	switch e.State {
	case "done", "discarded":
		return e, nil
	case "sending":
		return e, ErrInFlight
	case "uncertain":
		return e, ErrUncertain
	}
	e.State = "sending"
	f.effects[key] = e
	return e, nil
}
func (f *workerFixture) ResolveEffect(_ context.Context, _ Job, key string, state State, result json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.resolveErr != nil {
		return f.resolveErr
	}
	e := f.effects[key]
	e.State = string(state)
	e.Result = append(json.RawMessage(nil), result...)
	f.effects[key] = e
	return nil
}
func (f *workerFixture) DiscardEffect(_ context.Context, _ Job, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.effects[key]
	if e.State != "pending" {
		return ErrInvalid
	}
	e.State = "discarded"
	f.effects[key] = e
	return nil
}
func (f *workerFixture) SetJobState(_ context.Context, _ Job, state State, delay time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, e := range f.effects {
		if state == Uncertain && e.State == "sending" {
			e.State = "uncertain"
			f.effects[k] = e
		}
		if state != Uncertain && e.State == "uncertain" {
			return ErrUncertain
		}
		if state != Uncertain && e.State == "sending" {
			return ErrInFlight
		}
		if state == Done && e.State == "pending" {
			return ErrIncomplete
		}
	}
	f.state = state
	f.delay = delay
	return nil
}
func options() WorkerOptions {
	return WorkerOptions{Lease: 300 * time.Millisecond, RenewEvery: 30 * time.Millisecond, StoreTimeout: 20 * time.Millisecond, PollEvery: time.Millisecond, ShutdownTimeout: 50 * time.Millisecond}
}
func runOne(t *testing.T, f *workerFixture, h Handler) Report {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reports := make(chan Report, 2)
	c := options()
	c.Report = func(r Report) { reports <- r }
	w, err := NewWorker(f, map[string]Handler{"fixture": h}, c)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	select {
	case r := <-reports:
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not report")
		return Report{}
	}
}

func TestWorkerClassifiesProviderOutcomesAndHonorsRateDelay(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		state State
		delay time.Duration
	}{
		{"success", nil, Done, 0},
		{"rate limit", &providers.Error{Service: "telegram", Status: 429, Retry: true, RetryAfter: 17 * time.Second}, Pending, 17 * time.Second},
		{"definite refusal", &providers.Error{Service: "telegram", Status: 400}, Failed, 0},
		{"ambiguous send", &providers.Error{Service: "telegram", Status: 500, Uncertain: true, Retry: true}, Uncertain, 0},
		{"internal failure", errors.New("private-provider-token and body"), Uncertain, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixtureWorkerStore()
			r := runOne(t, f, func(ctx context.Context, j Job, e *Effects) error {
				_, err := e.Do("send", json.RawMessage(`{"chat":"original"}`), func(context.Context, json.RawMessage) (json.RawMessage, error) {
					return json.RawMessage(`{"id":7}`), tc.err
				})
				return err
			})
			if r.State != tc.state || r.Delay != tc.delay {
				t.Fatal(r)
			}
			if strings.Contains(fmt.Sprint(r), "private") {
				t.Fatal("unsafe report", r)
			}
		})
	}
}

func TestWorkerDoesNotCompleteIgnoredFailuresOrPermitMutationsAfterUncertainty(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		f := fixtureWorkerStore()
		var later atomic.Int32
		r := runOne(t, f, func(ctx context.Context, j Job, e *Effects) error {
			_, _ = e.Do("first", json.RawMessage(`{}`), func(context.Context, json.RawMessage) (json.RawMessage, error) {
				return nil, &providers.Error{Service: "telegram", Status: 400, Uncertain: uncertain}
			})
			if uncertain {
				_, _ = e.Do("later", json.RawMessage(`{}`), func(context.Context, json.RawMessage) (json.RawMessage, error) {
					later.Add(1)
					return json.RawMessage(`{}`), nil
				})
			}
			return nil
		})
		want := Failed
		if uncertain {
			want = Uncertain
		}
		if r.State != want || later.Load() != 0 {
			t.Fatal(r, later.Load())
		}
	}
}

func TestExplicitDiscardAllowsDefinitiveRefusalNoticeToComplete(t *testing.T) {
	f := fixtureWorkerStore()
	r := runOne(t, f, func(ctx context.Context, j Job, e *Effects) error {
		_, err := e.Do("topic", json.RawMessage(`{}`), func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return nil, &providers.Error{Service: "telegram", Status: 403}
		})
		if err == nil {
			return errors.New("expected refusal")
		}
		if err := e.Discard("topic"); err != nil {
			return err
		}
		_, err = e.Do("notice", json.RawMessage(`{}`), func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"id":8}`), nil
		})
		return err
	})
	if r.State != Done || f.effects["topic"].State != "discarded" {
		t.Fatal(r, f.effects)
	}
}

func TestWorkerRejectsMalformedReceiptAndRecoversPanicsWithoutLeaking(t *testing.T) {
	for _, panics := range []bool{false, true} {
		f := fixtureWorkerStore()
		r := runOne(t, f, func(ctx context.Context, j Job, e *Effects) error {
			_, err := e.Do("send", json.RawMessage(`{}`), func(context.Context, json.RawMessage) (json.RawMessage, error) {
				if panics {
					panic("private-token")
				}
				return json.RawMessage(`broken`), nil
			})
			return err
		})
		if r.State != Uncertain || f.effects["send"].State != "uncertain" {
			t.Fatal(r)
		}
	}
}

func TestWorkerFreezesRequestsAndReusesCompletedReceipts(t *testing.T) {
	f := fixtureWorkerStore()
	f.effects["first"] = Effect{State: "done", Request: json.RawMessage(`{"chat":"old"}`), Result: json.RawMessage(`{"id":7}`)}
	f.effects["second"] = Effect{State: "pending", Request: json.RawMessage(`{"chat":"old"}`)}
	var called int
	r := runOne(t, f, func(ctx context.Context, j Job, e *Effects) error {
		result, err := e.Do("first", json.RawMessage(`{"chat":"new"}`), func(context.Context, json.RawMessage) (json.RawMessage, error) { called++; return nil, nil })
		if err != nil || string(result) != `{"id":7}` {
			return errors.New("receipt changed")
		}
		_, err = e.Do("second", json.RawMessage(`{"chat":"new"}`), func(_ context.Context, request json.RawMessage) (json.RawMessage, error) {
			called++
			if string(request) != `{"chat":"old"}` {
				return nil, ErrInvalid
			}
			return json.RawMessage(`{"id":8}`), nil
		})
		return err
	})
	if r.State != Done || called != 1 {
		t.Fatal(r, called)
	}
}

func TestWorkerCancelsOnLostLeaseAndDoesNotReleaseItsClaim(t *testing.T) {
	f := fixtureWorkerStore()
	f.renewErr = ErrClaimLost
	f.renewed = make(chan struct{})
	var called atomic.Int32
	r := runOne(t, f, func(ctx context.Context, j Job, e *Effects) error {
		<-ctx.Done()
		_, err := e.Do("late", json.RawMessage(`{}`), func(context.Context, json.RawMessage) (json.RawMessage, error) { called.Add(1); return nil, nil })
		return err
	})
	if r.Reason != "claim_lost" || f.state != "" || called.Load() != 0 {
		t.Fatal(r, f.state, called.Load())
	}
}

func TestWorkerRenewsWhileAnExternalOperationIsRunning(t *testing.T) {
	f := fixtureWorkerStore()
	f.renewed = make(chan struct{})
	r := runOne(t, f, func(ctx context.Context, j Job, e *Effects) error {
		_, err := e.Do("send", json.RawMessage(`{}`), func(context.Context, json.RawMessage) (json.RawMessage, error) {
			<-f.renewed
			return json.RawMessage(`{}`), nil
		})
		return err
	})
	if r.State != Done {
		t.Fatal(r)
	}
}

func TestWorkerBoundsShutdownForUncooperativeHandler(t *testing.T) {
	f := fixtureWorkerStore()
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := options()
	w, err := NewWorker(f, map[string]Handler{"fixture": func(context.Context, Job, *Effects) error { close(entered); <-release; return nil }}, c)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	<-entered
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("uncooperative handler was silently abandoned")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked")
	}
}

func TestWorkerAttemptLimitAndUnsupportedKindFailClosed(t *testing.T) {
	f := fixtureWorkerStore()
	f.job.Attempts = 8
	r := runOne(t, f, func(context.Context, Job, *Effects) error {
		return &providers.Error{Retry: true, RetryAfter: time.Hour}
	})
	if r.State != Failed {
		t.Fatal(r)
	}
	f = fixtureWorkerStore()
	f.job.Kind = "not-implemented"
	r = runOne(t, f, func(context.Context, Job, *Effects) error { t.Fatal("wrong handler called"); return nil })
	if r.State != Failed {
		t.Fatal(r)
	}
}

func TestIgnoredPersistenceFailureCannotCompleteJob(t *testing.T) {
	f := fixtureWorkerStore()
	f.beginErr = errors.New("private database credentials")
	r := runOne(t, f, func(ctx context.Context, j Job, e *Effects) error {
		_, _ = e.Do("send", json.RawMessage(`{}`), func(context.Context, json.RawMessage) (json.RawMessage, error) {
			t.Fatal("mutation after failed preparation")
			return nil, nil
		})
		return nil
	})
	if r.State != Uncertain {
		t.Fatal(r)
	}
}

func TestGracefulCancellationBeforeMutationReturnsPending(t *testing.T) {
	f := fixtureWorkerStore()
	entered := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := options()
	w, err := NewWorker(f, map[string]Handler{"fixture": func(ctx context.Context, _ Job, _ *Effects) error { close(entered); <-ctx.Done(); return ctx.Err() }}, c)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	<-entered
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if f.state != Pending {
		t.Fatal("unsent job unnecessarily uncertain", f.state)
	}
}

type blockedRecovery struct {
	*workerFixture
	entered chan struct{}
}

func (f *blockedRecovery) RecoverExpired(ctx context.Context) (int, error) {
	close(f.entered)
	<-ctx.Done()
	return 0, ctx.Err()
}
func TestCancellationDuringRecoveryIsCleanShutdown(t *testing.T) {
	f := &blockedRecovery{workerFixture: fixtureWorkerStore(), entered: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	w, err := NewWorker(f, map[string]Handler{"fixture": func(context.Context, Job, *Effects) error { return nil }}, options())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	<-f.entered
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
