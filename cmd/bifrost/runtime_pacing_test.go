package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/veryshyjelly/bifrost/internal/delivery"
)

type claimCounter struct {
	delivery.WorkerStore
	calls int
	err   error
}

func (s *claimCounter) Claim(context.Context, time.Duration) (delivery.Job, error) {
	s.calls++
	return delivery.Job{ID: int64(s.calls)}, s.err
}

func TestRuntimePacingLimitsClaimsWithoutSpendingLeases(t *testing.T) {
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	base := &claimCounter{}
	s := &runtimePacedStore{WorkerStore: base, interval: 1100 * time.Millisecond, now: func() time.Time { return now }}
	if _, err := s.Claim(t.Context(), time.Minute); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if _, err := s.Claim(t.Context(), time.Minute); !errors.Is(err, delivery.ErrNoJob) || base.calls != 1 {
		t.Fatal("pacing claimed a lease early", err)
	}
	now = now.Add(100 * time.Millisecond)
	base.err = delivery.ErrNoJob
	if _, err := s.Claim(t.Context(), time.Minute); !errors.Is(err, delivery.ErrNoJob) {
		t.Fatal(err)
	}
	base.err = nil
	if _, err := s.Claim(t.Context(), time.Minute); err != nil || base.calls != 3 {
		t.Fatal("empty queue consumed the next admission slot", err)
	}
}
