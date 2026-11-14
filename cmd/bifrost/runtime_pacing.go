package main

import (
	"context"
	"sync"
	"time"

	"github.com/veryshyjelly/bifrost/internal/delivery"
)

// Pace admissions between jobs, as the baseline queue tick did. Never sleep
// after a claim or beneath a provider attempt: that would consume its lease
// or turn cancellation before transmission into an uncertain send.
type runtimePacedStore struct {
	delivery.WorkerStore
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
	now      func() time.Time
}

func (s *runtimePacedStore) Claim(ctx context.Context, lease time.Duration) (delivery.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.now().Before(s.next) {
		return delivery.Job{}, delivery.ErrNoJob
	}
	job, err := s.WorkerStore.Claim(ctx, lease)
	if err == nil {
		s.next = s.now().Add(s.interval)
	}
	return job, err
}
