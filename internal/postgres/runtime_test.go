package postgres

import (
	"errors"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"testing"
	"time"
)

func TestRuntimeLeaseExcludesSecondOwnerAndReleasesItsSession(t *testing.T) {
	s := testStore(t)
	first, err := s.AcquireRuntimeLease(t.Context(), "fixture-account")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close(t.Context())
	if _, err = s.AcquireRuntimeLease(t.Context(), "fixture-account"); !errors.Is(err, ErrRuntimeHeld) {
		t.Fatal("second runtime acquired account", err)
	}
	if err = first.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = first.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = first.Check(t.Context()); !errors.Is(err, ErrRuntimeLeaseLost) {
		t.Fatal("closed ownership remained valid", err)
	}
	next, err := s.AcquireRuntimeLease(t.Context(), "fixture-account")
	if err != nil {
		t.Fatal(err)
	}
	if err = next.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapClaimLeavesOrdinaryIngressPending(t *testing.T) {
	s := testStore(t)
	if accepted, err := s.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: "ordinary", Stream: "wa:peer", Kind: "wa", Payload: []byte(`{}`)}); err != nil || !accepted {
		t.Fatal(err)
	}
	if accepted, err := s.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: "setup", Stream: "bot-settings:master", Kind: "control-bot-setup", Payload: []byte(`{}`)}); err != nil || !accepted {
		t.Fatal(err)
	}
	job, err := s.ClaimKinds(t.Context(), time.Minute, []string{"control-bot-setup"})
	if err != nil || job.Dedup != "setup" {
		t.Fatal("bootstrap consumed ordinary ingress", err)
	}
	finish(t, s, job)
	if _, err = s.ClaimKinds(t.Context(), time.Minute, []string{"control-bot-setup"}); !errors.Is(err, delivery.ErrNoJob) {
		t.Fatal("empty bootstrap filter consumed ingress", err)
	}
	job, err = s.Claim(t.Context(), time.Minute)
	if err != nil || job.Dedup != "ordinary" {
		t.Fatal("bootstrap changed ordinary queue", err)
	}
	finish(t, s, job)
	if _, err = s.ClaimKinds(t.Context(), time.Minute, nil); !errors.Is(err, delivery.ErrInvalid) {
		t.Fatal("empty selection enabled all claims", err)
	}
}

func TestRuntimeLeaseDetectsSessionLossAndRefusesUnpreparedIdentity(t *testing.T) {
	s := testStore(t)
	lease, err := s.AcquireRuntimeLease(t.Context(), "fixture-account")
	if err != nil {
		t.Fatal(err)
	}
	if err = lease.conn.Conn().Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(lease.Check(t.Context()), ErrRuntimeLeaseLost) {
		t.Fatal("dead session retained runtime ownership")
	}
	if !errors.Is(lease.Close(t.Context()), ErrRuntimeLeaseLost) {
		t.Fatal("unconfirmed unlock was returned as successful")
	}
	if err = lease.Close(t.Context()); err != nil {
		t.Fatal("repeated close failed", err)
	}
	if _, err = s.AcquireRuntimeLease(t.Context(), "foreign-account"); err == nil {
		t.Fatal("foreign account acquired runtime")
	}
}
