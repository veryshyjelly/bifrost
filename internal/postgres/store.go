package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

var (
	ErrIdentityConflict = errors.New("BIFROST_ACCOUNT_ID conflicts with the saved identity")
	ErrEncryptionKey    = errors.New("encryption key does not match the saved identity")
)

type Store struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// Open connects without migrating, initializing identity or recovering other workers.
func Open(ctx context.Context, url string) (*Store, error) {
	if url == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL connection configuration")
	}
	config.ConnConfig.RuntimeParams["application_name"] = "bifrost"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("cannot initialize PostgreSQL connection pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("PostgreSQL is unavailable")
	}
	return &Store{pool: pool, queries: db.New(pool)}, nil
}

func (s *Store) Close()                         { s.pool.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

// InitializeIdentity binds an empty PostgreSQL database to an identity and key.
// Legacy SQLite adoption belongs to the explicit importer, not connection startup.
func (s *Store) InitializeIdentity(ctx context.Context, account string, key [32]byte) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer rollback(tx)
	q := s.queries.WithTx(tx)
	if err = q.LockIdentity(ctx); err != nil {
		return "", err
	}
	fingerprint := sha256.Sum256(key[:])
	encoded := hex.EncodeToString(fingerprint[:])
	saved, err := q.Identity(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		if account == "" {
			account = rand.Text()
		}
		err = q.CreateIdentity(ctx, db.CreateIdentityParams{AccountID: account, KeyFingerprint: encoded})
	} else if err == nil {
		if account != "" && account != saved.AccountID {
			return "", ErrIdentityConflict
		}
		if encoded != saved.KeyFingerprint {
			return "", ErrEncryptionKey
		}
		account = saved.AccountID
	}
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return account, nil
}

func (s *Store) Enqueue(ctx context.Context, event delivery.Event) (bool, error) {
	if event.AccountID == "" || event.Dedup == "" || event.Stream == "" || event.Kind == "" || !json.Valid(event.Payload) {
		return false, delivery.ErrInvalid
	}
	_, err := s.queries.Enqueue(ctx, db.EnqueueParams{AccountID: event.AccountID, Dedup: event.Dedup, Stream: event.Stream, Kind: event.Kind, Payload: event.Payload})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func seconds(d time.Duration, positive bool) (int32, error) {
	if d < 0 || (positive && d == 0) {
		return 0, delivery.ErrInvalid
	}
	n := math.Ceil(d.Seconds())
	if n > math.MaxInt32 {
		return 0, delivery.ErrInvalid
	}
	return int32(n), nil
}

func jobFrom(row db.Job) delivery.Job {
	return delivery.Job{ID: row.ID, Token: row.ClaimToken, Attempts: row.Attempts, Event: delivery.Event{
		AccountID: row.AccountID, Dedup: row.Dedup, Stream: row.Stream, Kind: row.Kind, Payload: row.Payload,
	}}
}

func (s *Store) Claim(ctx context.Context, lease time.Duration) (delivery.Job, error) {
	n, err := seconds(lease, true)
	if err != nil {
		return delivery.Job{}, err
	}
	row, err := s.queries.Claim(ctx, db.ClaimParams{Token: rand.Text(), LeaseSeconds: n})
	if errors.Is(err, pgx.ErrNoRows) {
		return delivery.Job{}, delivery.ErrNoJob
	}
	return jobFrom(row), err
}

func (s *Store) Renew(ctx context.Context, job delivery.Job, lease time.Duration) error {
	n, err := seconds(lease, true)
	if err != nil {
		return err
	}
	count, err := s.queries.Renew(ctx, db.RenewParams{ID: job.ID, ClaimToken: job.Token, LeaseSeconds: n})
	if err == nil && count != 1 {
		return delivery.ErrClaimLost
	}
	return err
}

// withClaim fences all receipt/state writes against stale or expired workers.
func (s *Store) withClaim(ctx context.Context, job delivery.Job, action func(*db.Queries, db.Job) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	q := s.queries.WithTx(tx)
	row, err := q.LockClaim(ctx, db.LockClaimParams{ID: job.ID, ClaimToken: job.Token})
	if errors.Is(err, pgx.ErrNoRows) {
		return delivery.ErrClaimLost
	}
	if err != nil {
		return err
	}
	if row.AccountID != job.AccountID {
		return delivery.ErrClaimLost
	}
	if err = action(q, row); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// BeginEffect returns the original prepared request, or a completed receipt.
// Call the provider only when State is sending. A sending/uncertain replay errors.
func (s *Store) BeginEffect(ctx context.Context, job delivery.Job, key string, request json.RawMessage) (delivery.Effect, error) {
	if key == "" || !json.Valid(request) {
		return delivery.Effect{}, delivery.ErrInvalid
	}
	var effect delivery.Effect
	err := s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if err := q.CreateEffect(ctx, db.CreateEffectParams{AccountID: row.AccountID, EffectKey: key, JobID: row.ID, Request: request}); err != nil {
			return err
		}
		e, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: key})
		if err != nil {
			return err
		}
		effect = delivery.Effect{State: e.State, Request: e.Request, Result: e.Result}
		if e.State == "done" {
			return nil
		}
		if e.State == "uncertain" {
			return delivery.ErrUncertain
		}
		if e.State == "sending" {
			return delivery.ErrInFlight
		}
		if e.JobID != row.ID {
			return delivery.ErrInvalid
		}
		if err := q.StartEffect(ctx, db.StartEffectParams{AccountID: row.AccountID, EffectKey: key}); err != nil {
			return err
		}
		effect.State = "sending"
		return nil
	})
	return effect, err
}

// ResolveEffect records success, a proven non-delivery, or an ambiguous outcome.
func (s *Store) ResolveEffect(ctx context.Context, job delivery.Job, key string, state delivery.State, result json.RawMessage) error {
	if state != delivery.Done && state != delivery.Pending && state != delivery.Uncertain {
		return delivery.ErrInvalid
	}
	if result == nil {
		result = json.RawMessage("null")
	}
	if !json.Valid(result) {
		return delivery.ErrInvalid
	}
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		count, err := q.ResolveEffect(ctx, db.ResolveEffectParams{AccountID: row.AccountID, EffectKey: key, JobID: row.ID, State: string(state), Result: result})
		if err == nil && count != 1 {
			return delivery.ErrInvalid
		}
		return err
	})
}

func (s *Store) SetJobState(ctx context.Context, job delivery.Job, state delivery.State, delay time.Duration) error {
	if state != delivery.Done && state != delivery.Pending && state != delivery.Failed && state != delivery.Uncertain {
		return delivery.ErrInvalid
	}
	n, err := seconds(delay, false)
	if err != nil {
		return err
	}
	return s.withClaim(ctx, job, func(q *db.Queries, row db.Job) error {
		if state == delivery.Uncertain {
			if err := q.MarkSendingUncertain(ctx, row.ID); err != nil {
				return err
			}
		} else {
			uncertain, err := q.HasUncertainEffect(ctx, row.ID)
			if err != nil {
				return err
			}
			if uncertain {
				return delivery.ErrUncertain
			}
			sending, err := q.HasSendingEffect(ctx, row.ID)
			if err != nil {
				return err
			}
			if sending {
				return delivery.ErrInFlight
			}
		}
		if state == delivery.Done {
			count, err := q.UnfinishedEffects(ctx, row.ID)
			if err != nil {
				return err
			}
			if count != 0 {
				return delivery.ErrIncomplete
			}
		}
		return q.SetJobState(ctx, db.SetJobStateParams{ID: row.ID, State: string(state), DelaySeconds: n})
	})
}

// RecoverExpired never touches a live claim or replays an abandoned sending effect.
func (s *Store) RecoverExpired(ctx context.Context) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer rollback(tx)
	q := s.queries.WithTx(tx)
	rows, err := q.ExpiredJobs(ctx)
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		if err := q.MarkSendingUncertain(ctx, row.ID); err != nil {
			return 0, err
		}
		unknown, err := q.HasUncertainEffect(ctx, row.ID)
		if err != nil {
			return 0, err
		}
		state := delivery.Pending
		if unknown {
			state = delivery.Uncertain
		}
		if err := q.SetJobState(ctx, db.SetJobStateParams{ID: row.ID, State: string(state)}); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// Review requires an explicit confirmation before retrying an ambiguous send.
func (s *Store) Review(ctx context.Context, id int64, retry, confirmNoDelivery bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	q := s.queries.WithTx(tx)
	row, err := q.ReviewJob(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return delivery.ErrInvalid
	}
	if err != nil {
		return err
	}
	state := delivery.Done
	if retry {
		unknown, err := q.HasUncertainEffect(ctx, id)
		if err != nil {
			return err
		}
		if (row.State == "uncertain" || unknown) && !confirmNoDelivery {
			return delivery.ErrUncertain
		}
		if err := q.ResetUncertainEffects(ctx, id); err != nil {
			return err
		}
		state = delivery.Pending
	}
	if err := q.SetJobState(ctx, db.SetJobStateParams{ID: id, State: string(state)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Counts(ctx context.Context) (map[string]int64, error) {
	rows, err := s.queries.JobCounts(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[row.State] = row.Count
	}
	return counts, nil
}
