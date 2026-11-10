package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func executeWorker(t *testing.T, store delivery.WorkerStore, kind string, h delivery.Handler) delivery.Report {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reports := make(chan delivery.Report, 4)
	w, err := delivery.NewWorker(store, map[string]delivery.Handler{kind: h}, delivery.WorkerOptions{Lease: 3 * time.Second, RenewEvery: 300 * time.Millisecond, StoreTimeout: 500 * time.Millisecond, PollEvery: 5 * time.Millisecond, ShutdownTimeout: time.Second, Report: func(r delivery.Report) { reports <- r; cancel() }})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	select {
	case r := <-reports:
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("worker timeout")
		return delivery.Report{}
	}
}

func TestRealWorkerPartialRetryPreservesReceiptsAndFrozenDestinations(t *testing.T) {
	s := testStore(t)
	enqueue(t, s, "event-1", "chat")
	enqueue(t, s, "event-2", "chat")
	var first, second atomic.Int32
	h := func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		request := json.RawMessage(`{"destination":"old"}`)
		if j.Attempts > 1 {
			request = json.RawMessage(`{"destination":"changed"}`)
		}
		_, err := e.Do("first", request, func(context.Context, json.RawMessage) (json.RawMessage, error) {
			first.Add(1)
			return json.RawMessage(`{"message_id":7}`), nil
		})
		if err != nil {
			return err
		}
		_, err = e.Do("second", request, func(_ context.Context, frozen json.RawMessage) (json.RawMessage, error) {
			if string(frozen) != `{"destination": "old"}` && string(frozen) != `{"destination":"old"}` {
				t.Error("frozen destination changed", string(frozen))
			}
			if second.Add(1) == 1 {
				return nil, &providers.Error{Service: "telegram", Status: 429, Retry: true, RetryAfter: time.Minute}
			}
			return json.RawMessage(`{"message_id":8}`), nil
		})
		return err
	}
	r := executeWorker(t, s, "fixture", h)
	if r.State != delivery.Pending || r.Delay != time.Minute {
		t.Fatal(r)
	}
	if _, err := s.Claim(t.Context(), time.Second); !errors.Is(err, delivery.ErrNoJob) {
		t.Fatal("later stream job overtook retry", err)
	}
	var due bool
	if err := s.pool.QueryRow(t.Context(), "SELECT due > clock_timestamp() + interval '55 seconds' FROM jobs WHERE id=$1", r.JobID).Scan(&due); err != nil || !due {
		t.Fatal("rate delay lost", err)
	}
	if _, err := s.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	r = executeWorker(t, s, "fixture", h)
	if r.State != delivery.Done || first.Load() != 1 || second.Load() != 2 {
		t.Fatal(r, first.Load(), second.Load())
	}
	if j := claim(t, s); j.Dedup != "event-2" {
		t.Fatal(j.Dedup)
	}
}

func TestRealWorkerUncertaintyRequiresReviewAndPreservesDiscardedBranches(t *testing.T) {
	s := testStore(t)
	enqueue(t, s, "event-1", "chat")
	enqueue(t, s, "event-2", "chat")
	r := executeWorker(t, s, "fixture", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		_, err := e.Do("send", json.RawMessage(`{}`), func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return nil, &providers.Error{Service: "telegram", Status: 500, Uncertain: true}
		})
		return err
	})
	if r.State != delivery.Uncertain {
		t.Fatal(r)
	}
	if err := s.Review(t.Context(), r.JobID, true, false); !errors.Is(err, delivery.ErrUncertain) {
		t.Fatal(err)
	}
	if _, err := s.Claim(t.Context(), time.Second); !errors.Is(err, delivery.ErrNoJob) {
		t.Fatal("uncertainty did not block stream")
	}
	if err := s.Review(t.Context(), r.JobID, true, true); err != nil {
		t.Fatal(err)
	}
	r = executeWorker(t, s, "fixture", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		_, err := e.Do("send", json.RawMessage(`{"changed":true}`), func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return nil, &providers.Error{Service: "telegram", Status: 403}
		})
		if err == nil {
			return errors.New("expected refusal")
		}
		return e.Discard("send")
	})
	if r.State != delivery.Done {
		t.Fatal(r)
	}
	var state string
	if err := s.pool.QueryRow(t.Context(), "SELECT state FROM effects WHERE effect_key='send'").Scan(&state); err != nil || state != "discarded" {
		t.Fatal(state, err)
	}
	j := claim(t, s)
	effect, err := s.BeginEffect(t.Context(), j, "send", json.RawMessage(`{}`))
	if err != nil || effect.State != "discarded" {
		t.Fatal(effect.State, err)
	}
	if err := s.ResolveEffect(t.Context(), j, "send", delivery.Done, json.RawMessage(`{}`)); !errors.Is(err, delivery.ErrInvalid) {
		t.Fatal("discarded effect changed", err)
	}
}

type committedReceiptFailure struct {
	*Store
	fail atomic.Bool
}

func (s *committedReceiptFailure) ResolveEffect(ctx context.Context, j delivery.Job, key string, state delivery.State, result json.RawMessage) error {
	if err := s.Store.ResolveEffect(ctx, j, key, state, result); err != nil {
		return err
	}
	if state == delivery.Done && s.fail.CompareAndSwap(true, false) {
		return errors.New("fixture connection lost after receipt commit")
	}
	return nil
}
func TestWorkerDoesNotResendAfterReceiptCommitResponseIsLost(t *testing.T) {
	s := testStore(t)
	enqueue(t, s, "event-1", "chat")
	wrapper := &committedReceiptFailure{Store: s}
	wrapper.fail.Store(true)
	var calls atomic.Int32
	h := func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		_, err := e.Do("send", json.RawMessage(`{}`), func(context.Context, json.RawMessage) (json.RawMessage, error) {
			calls.Add(1)
			return json.RawMessage(`{"message_id":7}`), nil
		})
		return err
	}
	r := executeWorker(t, wrapper, "fixture", h)
	if r.State != delivery.Uncertain {
		t.Fatal(r)
	}
	if err := s.Review(t.Context(), r.JobID, true, true); err != nil {
		t.Fatal(err)
	}
	r = executeWorker(t, s, "fixture", h)
	if r.State != delivery.Done || calls.Load() != 1 {
		t.Fatal(r, calls.Load())
	}
}
