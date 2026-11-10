package postgres

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/mentions"
)

func TestIdentityEvidenceIsFencedImmutableAndAccountScoped(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	enqueue(t, s, "observe", "identity")
	j := claim(t, s)
	if err := s.SaveContactIdentity(ctx, j, "987654321:2@lid", "12025550123@s.whatsapp.net", identity.Profile{ID: "12025550123@c.us", Name: "Saved Alice", Synced: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveContactIdentity(ctx, j, "987654321@lid", "12025550124@c.us", identity.Profile{ID: "12025550123@c.us", Name: "Wrong"}); !errors.Is(err, identity.ErrConflict) {
		t.Fatal(err)
	}
	if err := s.SaveContactIdentity(ctx, j, "", "", identity.Profile{ID: "12025550123@c.us", Name: "12025550123:7@c.us"}); err != nil {
		t.Fatal(err)
	}
	d, err := s.IdentityDirectory(ctx, j.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Canonical("987654321:9@lid") != "12025550123@c.us" || d.Person("987654321@lid", nil).Name != "Saved Alice" {
		t.Fatal("alias conflict or placeholder overwrote cached metadata")
	}
	if _, err := s.SaveMentionToken(ctx, j, "deadbeef", mentions.Token{ChatID: "g@g.us", PersonID: "987654321@lid"}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := s.MentionToken(ctx, "other-account", "deadbeef"); err != nil || found {
		t.Fatal(found, err)
	}
	expire(t, s, j)
	if err := s.SaveContactIdentity(ctx, j, "555555@lid", "12025550124@c.us", identity.Profile{}); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
	if _, err := s.SaveMentionToken(ctx, j, "cafebabe", mentions.Token{ChatID: "g@g.us", PersonID: "12025550124@c.us"}); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
	if _, found, err := s.MentionToken(ctx, j.AccountID, "cafebabe"); err != nil || found {
		t.Fatal("stale worker wrote a token", err)
	}
}

func TestCanonicalBindingCannotRaceWithAliasEvidence(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	for _, bot := range []string{"200", "201"} {
		if err := s.PutIngressBot(ctx, "fixture-account", ingress.Bot{ID: bot}); err != nil {
			t.Fatal(err)
		}
	}
	jobs := []delivery.Job{}
	for _, key := range []string{"bind-lid", "alias", "bind-phone"} {
		if _, err := s.Enqueue(ctx, delivery.Event{AccountID: "fixture-account", Kind: "management", Dedup: key, Stream: key, Payload: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, claim(t, s))
	}
	if err := s.BindContactBot(ctx, jobs[0], "987654321@lid", "200"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	out := make(chan error, 2)
	wg.Go(func() {
		out <- s.SaveContactIdentity(ctx, jobs[1], "987654321@lid", "12025550123@c.us", identity.Profile{})
	})
	wg.Go(func() { out <- s.BindContactBot(ctx, jobs[2], "12025550123@c.us", "201") })
	wg.Wait()
	close(out)
	conflicts := 0
	for err := range out {
		if errors.Is(err, identity.ErrConflict) {
			conflicts++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if conflicts != 1 {
		t.Fatal("concurrent alias/binding merged identities", conflicts)
	}
	if _, err := s.IdentityDirectory(ctx, "fixture-account"); err != nil {
		t.Fatal("conflicting directory committed", err)
	}
}

func TestRosterSnapshotsAndTokenCollisionsRetainTheirOriginalMeaning(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	enqueue(t, s, "picker", "identity")
	j := claim(t, s)
	want := mentions.Token{ChatID: "g@g.us", PersonID: "12025550123@c.us"}
	if _, err := s.SaveMentionToken(ctx, j, "deadbeef", want); err != nil {
		t.Fatal(err)
	}
	got, err := s.SaveMentionToken(ctx, j, "deadbeef", mentions.Token{ChatID: "other@g.us", PersonID: "12025550124@c.us"})
	if err != nil || got != want {
		t.Fatal(got, err)
	}
	now := time.Now().UnixMilli()
	r := mentions.Roster{ChatID: "g@g.us", Name: "Current", FetchedAt: now, Members: []identity.Member{{ID: want.PersonID, Name: "Alice"}}}
	if err := s.SaveMentionRoster(ctx, j, r); err != nil {
		t.Fatal(err)
	}
	r.Name = "Old"
	r.FetchedAt--
	if err := s.SaveMentionRoster(ctx, j, r); err != nil {
		t.Fatal(err)
	}
	gotRoster, found, err := s.MentionRoster(ctx, j.AccountID, r.ChatID)
	if err != nil || !found || gotRoster.Name != "Current" {
		t.Fatal(gotRoster, found, err)
	}
}
