package postgres

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/ingress"
)

func sourceEvent(t *testing.T, key, chat string, extra map[string]any) delivery.Event {
	t.Helper()
	m := map[string]any{"id": key, "chatId": chat, "type": "text", "body": "Hello", "from": chat}
	for k, v := range extra {
		m[k] = v
	}
	payload, err := json.Marshal(map[string]any{"sessionId": "fixture-session", "accountId": "fixture-account", "event": sdk.EventMessageReceived, "idempotencyKey": key, "data": m})
	if err != nil {
		t.Fatal(err)
	}
	return delivery.Event{AccountID: "fixture-account", Dedup: "ow:" + key, Stream: "wa:" + chat, Kind: "wa", Payload: payload}
}
func admitSource(t *testing.T, s *Store, event delivery.Event) {
	t.Helper()
	accepted, err := s.Admit(t.Context(), &event, nil)
	if err != nil || !accepted {
		t.Fatal(accepted, err)
	}
}

func TestAdmissionFreezesSourceAndRetainsConversationAfterLearningPhone(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	lid := "987654321@lid"
	phone := "12025550123@c.us"
	first := sourceEvent(t, "first", lid, map[string]any{"contact": map[string]string{"name": "Alice"}})
	admitSource(t, s, first)
	j := claim(t, s)
	a, err := s.NormalizeIncoming(ctx, j)
	if err != nil || a.ChatID != lid || a.SenderID != lid || a.Name != "Alice" {
		t.Fatal(a, err)
	}
	finish(t, s, j)
	if err := s.PutIngressRoute(ctx, "fixture-account", ingress.Route{WhatsAppChat: lid, TelegramChatID: -200}); err != nil {
		t.Fatal(err)
	}
	second := sourceEvent(t, "second", lid, map[string]any{"senderPhone": "12025550123", "contact": map[string]string{"name": "New Alice", "number": "987654321"}})
	admitSource(t, s, second)
	third := sourceEvent(t, "third", phone, nil)
	admitSource(t, s, third)
	j = claim(t, s)
	b, err := s.NormalizeIncoming(ctx, j)
	if err != nil || b.ChatID != lid || b.SenderID != phone || b.LinkSenderID != phone || j.Stream != "wa:"+lid {
		t.Fatal(b, j.Stream, err)
	}
	if _, err := s.Claim(ctx, time.Second); !errors.Is(err, delivery.ErrNoJob) {
		t.Fatal("phone alias escaped the active stream", err)
	}
	if err := s.SaveContactIdentity(ctx, j, "", "", identity.Profile{ID: phone, Name: "Renamed", Synced: true}); err != nil {
		t.Fatal(err)
	}
	again, err := s.NormalizeIncoming(ctx, j)
	if err != nil || !reflect.DeepEqual(again, b) {
		t.Fatal("source snapshot changed", again, err)
	}
	finish(t, s, j)
	j = claim(t, s)
	c, err := s.NormalizeIncoming(ctx, j)
	if err != nil || c.ChatID != lid || j.Stream != "wa:"+lid || c.RawChatID != phone {
		t.Fatal(c, j.Stream, err)
	}
	var routes int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM bridge_routes").Scan(&routes); err != nil || routes != 1 {
		t.Fatal(routes, err)
	}
}
func TestSourceAdmissionAndEvidenceRollbackTogether(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	if _, err := s.pool.Exec(ctx, "ALTER TABLE whatsapp_sources ADD CONSTRAINT fixture_reject_source CHECK (false)"); err != nil {
		t.Fatal(err)
	}
	event := sourceEvent(t, "rollback", "987654321@lid", map[string]any{"senderPhone": "12025550123", "contact": map[string]string{"name": "Alice"}})
	if accepted, err := s.Admit(ctx, &event, nil); err == nil || accepted {
		t.Fatal("acknowledged missing source snapshot", accepted, err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM jobs)+(SELECT count(*) FROM contact_aliases)+(SELECT count(*) FROM contact_profiles)").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial source admission", count, err)
	}
}
func TestConflictingSourceEvidenceIsRetainedWithoutPartialAliasWrites(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	for _, chat := range []string{"987654321@lid", "12025550123@c.us"} {
		if err := s.PutIngressRoute(ctx, "fixture-account", ingress.Route{WhatsAppChat: chat, TelegramChatID: -200}); err != nil {
			t.Fatal(err)
		}
	}
	event := sourceEvent(t, "conflict", "987654321@lid", map[string]any{"senderPhone": "12025550123", "contact": map[string]string{"name": "Alice"}})
	admitSource(t, s, event)
	j := claim(t, s)
	if _, err := s.NormalizeIncoming(ctx, j); !errors.Is(err, identity.ErrConflict) {
		t.Fatal(err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM whatsapp_sources)+(SELECT count(*) FROM contact_aliases)+(SELECT count(*) FROM contact_profiles)").Scan(&count); err != nil || count != 0 {
		t.Fatal("conflict committed partial identity", count, err)
	}
}
func TestRecoveredOlderJobCannotOvertakeAnActiveCanonicalStream(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	lid := "987654321@lid"
	phone := "12025550123@c.us"
	if err := s.PutIngressRoute(ctx, "fixture-account", ingress.Route{WhatsAppChat: lid, TelegramChatID: -200}); err != nil {
		t.Fatal(err)
	}
	older := sourceEvent(t, "older", phone, nil)
	newer := sourceEvent(t, "newer", lid, map[string]any{"senderPhone": "12025550123"})
	// Enqueue bypasses new admission to represent work queued before source
	// snapshots existed. Both raw streams have already been claimed.
	for _, e := range []delivery.Event{older, newer} {
		if _, err := s.Enqueue(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	oldJob, newJob := claim(t, s), claim(t, s)
	if _, err := s.NormalizeIncoming(ctx, newJob); err != nil {
		t.Fatal(err)
	}
	source, err := s.NormalizeIncoming(ctx, oldJob)
	if !errors.Is(err, delivery.ErrDeferred) || source.ChatID != lid {
		t.Fatal(source, err)
	}
	if _, err := s.Claim(ctx, time.Second); !errors.Is(err, delivery.ErrNoJob) {
		t.Fatal("older moved job ran concurrently with newer active job", err)
	}
	finish(t, s, newJob)
	resumed := claim(t, s)
	if resumed.ID != oldJob.ID || resumed.Dedup != oldJob.Dedup || resumed.Attempts != 1 || resumed.Stream != "wa:"+lid {
		t.Fatal(resumed.ID, resumed.Dedup, resumed.Attempts, resumed.Stream)
	}
	if _, err := s.NormalizeIncoming(ctx, resumed); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSelfIdentity(ctx, oldJob, "12025550999@c.us"); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal("old token survived transition", err)
	}
}
func TestDuplicateAdmissionCannotRewriteSourceProfiles(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	event := sourceEvent(t, "duplicate", "12025550123@c.us", map[string]any{"contact": map[string]string{"name": "Alice"}})
	admitSource(t, s, event)
	changed := sourceEvent(t, "duplicate", "12025550123@c.us", map[string]any{"contact": map[string]string{"name": "Changed"}})
	if accepted, err := s.Admit(ctx, &changed, nil); err != nil || accepted {
		t.Fatal(accepted, err)
	}
	d, err := s.IdentityDirectory(ctx, "fixture-account")
	if err != nil || d.SenderName("12025550123@c.us") != "Alice" {
		t.Fatal("duplicate rewrote metadata", err)
	}
}

func TestVerifiedSelfIdentityCannotBeReplacedByAnotherPhone(t *testing.T) {
	s := testStore(t)
	enqueue(t, s, "self", "self")
	j := claim(t, s)
	if err := s.SaveSelfIdentity(t.Context(), j, "12025550999@c.us"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSelfIdentity(t.Context(), j, "12025550888@c.us"); !errors.Is(err, identity.ErrConflict) {
		t.Fatal(err)
	}
	id, found, err := s.SelfIdentity(t.Context(), j.AccountID)
	if err != nil || !found || id != "12025550999@c.us" {
		t.Fatal(id, found, err)
	}
	if _, found, err := s.SelfIdentity(t.Context(), "other-account"); err != nil || found {
		t.Fatal("self identity crossed accounts", found, err)
	}
}
