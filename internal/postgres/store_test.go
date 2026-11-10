package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("BIFROST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set BIFROST_TEST_DATABASE_URL to an isolated PostgreSQL instance")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("test database configuration invalid")
	}
	t.Cleanup(admin.Close)
	schema := "bifrost_test_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		if err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("test URL invalid")
	}
	query := u.Query()
	query.Set("search_path", schema)
	query.Set("pool_max_conns", "4")
	u.RawQuery = query.Encode()
	store, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal("migration replay:", err)
	}
	if _, err := store.InitializeIdentity(ctx, "fixture-account", [32]byte{}); err != nil {
		t.Fatal(err)
	}
	return store
}

func enqueue(t *testing.T, s *Store, key, stream string) {
	t.Helper()
	accepted, err := s.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: key, Stream: stream, Kind: "fixture", Payload: json.RawMessage(`{"text":"hello"}`)})
	if err != nil || !accepted {
		t.Fatalf("enqueue accepted=%v err=%v", accepted, err)
	}
}
func claim(t *testing.T, s *Store) delivery.Job {
	t.Helper()
	j, err := s.Claim(t.Context(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func finish(t *testing.T, s *Store, j delivery.Job) {
	t.Helper()
	if err := s.SetJobState(t.Context(), j, delivery.Done, 0); err != nil {
		t.Fatal(err)
	}
}
func expire(t *testing.T, s *Store, j delivery.Job) {
	t.Helper()
	if _, err := s.pool.Exec(t.Context(), "UPDATE jobs SET lease_until=now()-interval '1 second' WHERE id=$1", j.ID); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityIsPermanentAndKeyBound(t *testing.T) {
	s := testStore(t)
	id, err := s.InitializeIdentity(t.Context(), "", [32]byte{})
	if err != nil || id != "fixture-account" {
		t.Fatal("identity changed")
	}
	if _, err := s.InitializeIdentity(t.Context(), "another-account", [32]byte{}); !errors.Is(err, ErrIdentityConflict) {
		t.Fatal("identity conflict accepted")
	}
	key := [32]byte{1}
	if _, err := s.InitializeIdentity(t.Context(), "", key); !errors.Is(err, ErrEncryptionKey) {
		t.Fatal("wrong encryption key accepted")
	}
}

func TestConcurrentAdmissionAndClaimPreserveStreamHeads(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	var wg sync.WaitGroup
	accepted := make(chan bool, 12)
	errs := make(chan error, 12)
	for range 12 {
		wg.Go(func() {
			ok, err := s.Enqueue(ctx, delivery.Event{AccountID: "fixture-account", Dedup: "same", Stream: "a", Kind: "fixture", Payload: json.RawMessage(`{}`)})
			accepted <- ok
			errs <- err
		})
	}
	wg.Wait()
	close(accepted)
	close(errs)
	count := 0
	for ok := range accepted {
		if ok {
			count++
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatalf("admitted %d duplicate jobs", count)
	}
	enqueue(t, s, "a2", "a")
	enqueue(t, s, "b1", "b")
	jobs := make(chan delivery.Job, 2)
	claimErrors := make(chan error, 2)
	for range 2 {
		wg.Go(func() { j, err := s.Claim(ctx, time.Minute); jobs <- j; claimErrors <- err })
	}
	wg.Wait()
	close(jobs)
	close(claimErrors)
	for err := range claimErrors {
		if err != nil {
			t.Fatal(err)
		}
	}
	streams := map[string]bool{}
	for j := range jobs {
		if streams[j.Stream] {
			t.Fatal("same stream claimed twice")
		}
		streams[j.Stream] = true
		defer finish(t, s, j)
	}
	if _, err := s.Claim(ctx, time.Minute); !errors.Is(err, delivery.ErrNoJob) {
		t.Fatal("later same-stream job overtook active head")
	}
}

func TestPartialDeliveryRetryUsesFrozenPayloadAndReceipts(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	enqueue(t, s, "first", "chat")
	j := claim(t, s)
	if _, err := s.BeginEffect(ctx, j, "label", json.RawMessage(`{"target":"original"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveEffect(ctx, j, "label", delivery.Done, json.RawMessage(`{"message_id":7}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginEffect(ctx, j, "media", json.RawMessage(`{"bytes":"original"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveEffect(ctx, j, "media", delivery.Pending, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetJobState(ctx, j, delivery.Pending, 0); err != nil {
		t.Fatal(err)
	}
	j = claim(t, s)
	e, err := s.BeginEffect(ctx, j, "label", json.RawMessage(`{"target":"changed"}`))
	if err != nil || e.State != "done" || string(e.Result) != `{"message_id": 7}` {
		t.Fatalf("completed receipt: %+v %v", e, err)
	}
	e, err = s.BeginEffect(ctx, j, "media", json.RawMessage(`{"bytes":"changed"}`))
	if err != nil || string(e.Request) != `{"bytes": "original"}` {
		t.Fatal("prepared media changed on retry")
	}
	if err := s.ResolveEffect(ctx, j, "label", delivery.Pending, nil); !errors.Is(err, delivery.ErrInvalid) {
		t.Fatal("completed effect was mutable")
	}
	if err := s.SetJobState(ctx, j, delivery.Done, 0); !errors.Is(err, delivery.ErrInFlight) {
		t.Fatal("unfinished effect completed job")
	}
	if err := s.ResolveEffect(ctx, j, "media", delivery.Done, nil); err != nil {
		t.Fatal(err)
	}
	finish(t, s, j)
}

func TestExpiredSendRequiresReviewAndFencesStaleWorker(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	enqueue(t, s, "first", "chat")
	enqueue(t, s, "second", "chat")
	enqueue(t, s, "other", "other")
	j := claim(t, s)
	if _, err := s.BeginEffect(ctx, j, "send:stable-key", json.RawMessage(`{"body":"hello"}`)); err != nil {
		t.Fatal(err)
	}
	if n, err := s.RecoverExpired(ctx); err != nil || n != 0 {
		t.Fatal("live worker recovered")
	}
	expire(t, s, j)
	if n, err := s.RecoverExpired(ctx); err != nil || n != 1 {
		t.Fatalf("recovered=%d error=%v", n, err)
	}
	if err := s.ResolveEffect(ctx, j, "send:stable-key", delivery.Done, nil); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal("stale receipt accepted")
	}
	if err := s.Renew(ctx, j, time.Minute); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal("expired worker renewed")
	}
	other := claim(t, s)
	if other.Stream != "other" {
		t.Fatal("uncertain stream did not block")
	}
	finish(t, s, other)
	if err := s.Review(ctx, j.ID, true, false); !errors.Is(err, delivery.ErrUncertain) {
		t.Fatal("unconfirmed uncertain retry accepted")
	}
	if err := s.Review(ctx, j.ID, true, true); err != nil {
		t.Fatal(err)
	}
	retried := claim(t, s)
	if retried.ID != j.ID || retried.Token == j.Token {
		t.Fatal("retry claim identity invalid")
	}
	e, err := s.BeginEffect(ctx, retried, "send:stable-key", json.RawMessage(`{"body":"changed"}`))
	if err != nil || string(e.Request) != `{"body": "hello"}` {
		t.Fatal("review changed prepared send")
	}
	if err := s.ResolveEffect(ctx, retried, "send:stable-key", delivery.Done, nil); err != nil {
		t.Fatal(err)
	}
	finish(t, s, retried)
	if next := claim(t, s); next.Dedup != "second" {
		t.Fatal("stream did not resume")
	}
}

func TestKnownFailureAndReviewSkipDoNotBlockOtherStreams(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	enqueue(t, s, "first", "chat")
	enqueue(t, s, "second", "chat")
	j := claim(t, s)
	if err := s.SetJobState(ctx, j, delivery.Failed, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ctx, time.Minute); !errors.Is(err, delivery.ErrNoJob) {
		t.Fatal("failed head overtaken")
	}
	if err := s.Review(ctx, j.ID, false, false); err != nil {
		t.Fatal(err)
	}
	if next := claim(t, s); next.Dedup != "second" {
		t.Fatal("skip did not resume stream")
	}
}

func TestExpiredReadOnlyJobRecoversWithoutResettingLiveClaims(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	enqueue(t, s, "read", "chat")
	j := claim(t, s)
	if err := s.Renew(ctx, j, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	expire(t, s, j)
	if n, err := s.RecoverExpired(ctx); err != nil || n != 1 {
		t.Fatal("abandoned read not recovered")
	}
	if next := claim(t, s); next.ID != j.ID || next.Attempts != 2 {
		t.Fatal("job attempts or identity lost")
	}
}

func TestNewConnectionDoesNotResetLiveEffectAndCrashAfterReceiptDoesNotResend(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	enqueue(t, s, "send", "chat")
	j := claim(t, s)
	if _, err := s.BeginEffect(ctx, j, "stable", json.RawMessage(`{"target":"original"}`)); err != nil {
		t.Fatal(err)
	}
	second, err := Open(ctx, s.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if n, err := second.RecoverExpired(ctx); err != nil || n != 0 {
		t.Fatal("second connection reset live claim")
	}
	if _, err := s.BeginEffect(ctx, j, "stable", json.RawMessage(`{}`)); !errors.Is(err, delivery.ErrInFlight) {
		t.Fatal("in-flight effect reissued")
	}
	if err := s.SetJobState(ctx, j, delivery.Pending, 0); !errors.Is(err, delivery.ErrInFlight) {
		t.Fatal("sending effect moved to automatic retry")
	}
	if err := s.ResolveEffect(ctx, j, "stable", delivery.Done, json.RawMessage(`{"receipt":"saved"}`)); err != nil {
		t.Fatal(err)
	}
	expire(t, s, j)
	if n, err := second.RecoverExpired(ctx); err != nil || n != 1 {
		t.Fatal("crashed receipt job not recovered")
	}
	recovered := claim(t, s)
	e, err := s.BeginEffect(ctx, recovered, "stable", json.RawMessage(`{"target":"changed"}`))
	if err != nil || e.State != "done" || string(e.Request) != `{"target": "original"}` {
		t.Fatal("completed receipt replayed after crash")
	}
	finish(t, s, recovered)
}

func TestRateLimitDelayHoldsOnlyItsOwnStream(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	enqueue(t, s, "first", "chat")
	enqueue(t, s, "second", "chat")
	j := claim(t, s)
	if err := s.SetJobState(ctx, j, delivery.Pending, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ctx, time.Minute); !errors.Is(err, delivery.ErrNoJob) {
		t.Fatal("retry delay or stream order ignored")
	}
	enqueue(t, s, "other", "other")
	other := claim(t, s)
	if other.Stream != "other" {
		t.Fatal("other stream blocked")
	}
	finish(t, s, other)
	if _, err := s.pool.Exec(ctx, "UPDATE jobs SET due=now()-interval '1 second' WHERE id=$1", j.ID); err != nil {
		t.Fatal(err)
	}
	if next := claim(t, s); next.ID != j.ID {
		t.Fatal("retry lost original job")
	}
}

func TestChatJobsHavePriorityOverMetadataWithoutOvertakingStreamHeads(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	_, err := s.Enqueue(ctx, delivery.Event{AccountID: "fixture-account", Dedup: "metadata", Stream: "metadata", Kind: "sync-group", Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	enqueue(t, s, "chat", "chat")
	j := claim(t, s)
	if j.Dedup != "chat" {
		t.Fatal("metadata delayed chat work")
	}
	finish(t, s, j)
	if next := claim(t, s); next.Dedup != "metadata" {
		t.Fatal("metadata lost")
	}
}
