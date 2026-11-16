package postgres

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/veryshyjelly/bifrost/internal/bots"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

var ErrRuntimeHeld = errors.New("another Go runtime owns this account")
var ErrRuntimeLeaseLost = errors.New("runtime account ownership was lost")

func (s *Store) RuntimeCredentials(ctx context.Context, account string) ([]bots.Credential, error) {
	rows, err := s.queries.BotCredentials(ctx, account)
	if err != nil {
		return nil, err
	}
	credentials := make([]bots.Credential, 0, len(rows))
	for _, row := range rows {
		credential, err := decodeCredential(row)
		if err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	return credentials, nil
}

func (s *Store) RuntimeBotReady(ctx context.Context, account, bot, configuration string, webhook bool) (bool, error) {
	if account == "" || bot == "" || configuration == "" {
		return false, delivery.ErrInvalid
	}
	step := int32(2)
	if webhook {
		step = 3
	}
	return s.queries.RuntimeBotReady(ctx, db.RuntimeBotReadyParams{AccountID: account, BotID: bot, Configuration: configuration, MinimumStep: step})
}

func (s *Store) RuntimeBotsReady(ctx context.Context, account, configuration string) (bool, error) {
	ready, err := s.queries.RuntimeBotsReady(ctx, db.RuntimeBotsReadyParams{AccountID: account, Configuration: configuration})
	return ready.Bool && ready.Valid, err
}
func (s *Store) RuntimeBotSetupNeedsReview(ctx context.Context, account string) (bool, error) {
	return s.queries.RuntimeBotSetupNeedsReview(ctx, account)
}

// RuntimeLease reserves a dedicated PostgreSQL session for account ownership.
// It must remain held until worker/scheduler/provider work has stopped.
type RuntimeLease struct {
	mu      sync.Mutex
	conn    *pgxpool.Conn
	account string
	closed  bool
}

// AcquireRuntimeLease verifies saved identity and retained-state readiness, then
// takes the account's session lock on a dedicated pooled connection. The caller
// must keep the returned lease until all active runtime work has stopped.
func (s *Store) AcquireRuntimeLease(ctx context.Context, account string) (*RuntimeLease, error) {
	if account == "" {
		return nil, delivery.ErrInvalid
	}
	identity, err := s.queries.Identity(ctx)
	if err != nil {
		return nil, err
	}
	if identity.AccountID != account {
		return nil, delivery.ErrInvalid
	}
	if err = s.RetainedStateReady(ctx, account); err != nil {
		return nil, err
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	locked, err := db.New(conn).TryRuntimeLease(ctx, account)
	if err != nil || !locked {
		conn.Release()
		if err != nil {
			return nil, err
		}
		return nil, ErrRuntimeHeld
	}
	return &RuntimeLease{conn: conn, account: account}, nil
}

// Check detects a closed lease or lost owning database session. It does not
// reacquire ownership after a connection failure.
func (l *RuntimeLease) Check(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.conn.Conn().Ping(ctx) != nil {
		return ErrRuntimeLeaseLost
	}
	return nil
}

// Close removes the session lock before returning its connection to the pool.
// If unlock cannot be confirmed, the session is closed rather than pooled with
// a potentially live lock. Calling Close again is harmless.
func (l *RuntimeLease) Close(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	unlocked, err := db.New(l.conn).ReleaseRuntimeLease(ctx, l.account)
	if err != nil || !unlocked {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = l.conn.Conn().Close(closeCtx)
		cancel()
		l.conn.Release()
		return ErrRuntimeLeaseLost
	}
	l.conn.Release()
	return nil
}
