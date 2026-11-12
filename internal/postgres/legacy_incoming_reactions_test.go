package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/reactions"
)

func legacyReactionCacheKeyForPG(chat, message, destination string) string {
	sum := sha256.Sum256([]byte(destination))
	return legacy.OutgoingReactionKey("fixture-account", chat, message) + ":" + hex.EncodeToString(sum[:])
}

func legacyIncomingReactionSQL() string {
	echo, _ := json.Marshal(map[string]any{"sender": "12025550999@c.us", "bot": "master", "chat": "-100", "message": 11, "emoji": "👍", "at": time.Now().UnixMilli()})
	return legacyInteractionSQL() +
		legacyValueSQL("wa-reaction-state", legacy.OutgoingReactionKey("fixture-account", "12345@g.us", "group-source"), `{"event":"ow:before-import","revision":5,"senders":{"12025550999@c.us":{"emoji":"👍","order":1,"echo":`+string(echo)+`},"888888@lid":{"emoji":"❤","order":4}}}`) +
		legacyValueSQL("reaction-echo", legacy.OutgoingReactionKey("fixture-account", "12345@g.us", "group-source"), string(echo)) +
		legacyValueSQL("tg-reaction-state", legacyReactionCacheKeyForPG("12345@g.us", "group-source", "*:-100:44"), `{"applied":{"master":"[11,\"❤\"]"},"refused":{"77":"[11,\"👎\"]"},"refusedAt":{"77":1700000000123},"refusedReason":{"77":"message_missing"},"pendingFallback":"ow:pending-fallback"}`) +
		legacyValueSQL("tg-reaction-state", legacyReactionCacheKeyForPG("999999@lid", "private-owner", "77:123:0"), `{"applied":{"77":"[42,\"👍\"]"},"refused":{}}`) +
		legacyValueSQL("tg-reaction-state", legacyReactionCacheKeyForPG("12025550123@c.us", "private-master", "master:123:0"), `{"applied":{"master":"[42,\"👎\"]"},"refused":{}}`) +
		`INSERT INTO jobs VALUES(101,'ow:pending-fallback','wa:12345@g.us','wa','{"event":"message.reaction","data":{"chatId":"12345@g.us","messageId":"group-source","senderId":"888888@lid","reaction":"❤"}}','running',2,0,NULL,1700000000123);`
}

func prepareIncomingReactionFixture(t *testing.T, s *Store, source string) *legacy.Bundle {
	t.Helper()
	b := prepareChangeFixture(t, s, source)
	if _, err := s.ProjectLegacyChanges(t.Context(), b.Account); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacyInteractions(t.Context(), b.Account); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLegacyIncomingReactionProjectionFeedsNativeStateEchoAndDestinationCaches(t *testing.T) {
	s := testStore(t)
	b := prepareIncomingReactionFixture(t, s, legacyIncomingReactionSQL())
	result, err := s.ProjectLegacyIncomingReactions(t.Context(), b.Account)
	if err != nil || result.States != 1 || result.Echoes != 1 || result.Caches != 3 || result.PendingFallback != 1 {
		t.Fatal("incoming reaction projection incomplete", err)
	}
	for _, tc := range []struct{ source, destination, bot, emoji string }{
		{"private-owner", "77:123:0", "77", "👍"}, {"private-master", "master:123:0", "master", "👎"},
	} {
		raw, err := s.queries.ReactionCache(t.Context(), db.ReactionCacheParams{AccountID: b.Account, WhatsappChat: "12025550123@c.us", MessageID: tc.source, DestinationKey: tc.destination})
		var cache reactions.Cache
		if err != nil || json.Unmarshal(raw, &cache) != nil || len(cache.Applied) != 1 || cache.Applied[tc.bot].Emoji != tc.emoji || cache.Applied[tc.bot].MessageID != 42 {
			t.Fatal("overlapping private reaction IDs crossed bot/source scope", err)
		}
	}
	var pending, sourceState string
	if err := s.pool.QueryRow(t.Context(), "SELECT legacy_job_key FROM legacy_pending_reaction_notices").Scan(&pending); err != nil || pending != "ow:pending-fallback" {
		t.Fatal("unfinished original fallback notice dropped", err)
	}
	if err := s.pool.QueryRow(t.Context(), "SELECT convert_from(raw_row,'UTF8')::jsonb->>'state' FROM legacy_rows WHERE source_table='jobs' AND convert_from(raw_row,'UTF8')::jsonb->>'dedup'='ow:pending-fallback'").Scan(&sourceState); err != nil || sourceState != "running" {
		t.Fatal("original unfinished job state changed", err)
	}
	var jobs, effects int
	if err := s.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM jobs),(SELECT count(*) FROM effects)").Scan(&jobs, &effects); err != nil || jobs != 0 || effects != 0 || !errors.Is(s.LegacyRuntimeReady(t.Context(), b.Account), ErrLegacyPending) {
		t.Fatal("reaction facts became runnable/confirmed operations", err)
	}
	if _, err := s.pool.Exec(t.Context(), "UPDATE legacy_imports SET activated_at=now()"); err != nil {
		t.Fatal(err)
	}
	job := legacyConsumerJob(t, s, "own-reaction-after-import", "message.reaction", map[string]any{"chatId": "12345@g.us", "messageId": "group-source", "senderId": "12025550999@c.us", "reaction": "👍"})
	input, err := s.ReactionInput(t.Context(), job, reactions.Settings{Session: "fixture-session", OwnerID: 123, NoticeTTL: 60})
	if err != nil || input.State.Revision != 6 || input.State.Full || input.State.Actors["888888@lid"].Order != 4 || input.State.Actors["12025550999@c.us"].Echo == nil || len(input.Destinations) != 1 || input.Destinations[0].MessageID != 11 {
		t.Fatal("native reaction input lost retained order, echo or newest destination", err)
	}
	cache := input.Caches["*:-100:44"]
	if cache.Applied["master"].Emoji != "❤" || cache.Refused["77"].At != 1700000000123 || cache.Refused["77"].Reason != "message_missing" {
		t.Fatal("native handling lost confirmed setting/refusal facts")
	}
	finish(t, s, job)
	replay, err := s.ProjectLegacyIncomingReactions(t.Context(), b.Account)
	if err != nil || !reflect.DeepEqual(result, replay) {
		t.Fatal("reaction projection replay changed its result", err)
	}
	raw, err := s.queries.ReactionState(t.Context(), db.ReactionStateParams{AccountID: b.Account, WhatsappChat: "12345@g.us", MessageID: "group-source"})
	var state reactions.State
	if err != nil || json.Unmarshal(raw, &state) != nil || state.Revision != 6 || state.Event != job.Dedup {
		t.Fatal("projection replay reset later native actor state", err)
	}
}

func TestLegacyIncomingReactionProjectionRollbackAndConflictingNativeState(t *testing.T) {
	s := testStore(t)
	b := prepareIncomingReactionFixture(t, s, legacyIncomingReactionSQL())
	_, err := s.pool.Exec(t.Context(), `CREATE FUNCTION reject_incoming_reaction_projection() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END; $$;
CREATE TRIGGER reject_incoming_reaction_projection BEFORE INSERT ON legacy_incoming_reaction_projections FOR EACH ROW EXECUTE FUNCTION reject_incoming_reaction_projection();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacyIncomingReactions(t.Context(), b.Account); err == nil {
		t.Fatal("failed incoming reaction checkpoint committed")
	}
	for _, table := range []string{"message_reactions", "reaction_destinations", "reaction_echoes", "legacy_pending_reaction_notices", "legacy_incoming_reaction_projections"} {
		var n int
		if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
			t.Fatal("partial incoming reaction state survived", table, err)
		}
	}
	if _, err := s.pool.Exec(t.Context(), "DROP TRIGGER reject_incoming_reaction_projection ON legacy_incoming_reaction_projections"); err != nil {
		t.Fatal(err)
	}
	if err := s.queries.SaveReactionState(t.Context(), db.SaveReactionStateParams{AccountID: b.Account, WhatsappChat: "12345@g.us", MessageID: "other", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacyIncomingReactions(t.Context(), b.Account); !errors.Is(err, ErrLegacyDestination) {
		t.Fatal("incoming projection overwrote native state", err)
	}
	if _, err := s.ProjectLegacyIncomingReactions(t.Context(), "foreign-account"); !errors.Is(err, ErrLegacyDestination) {
		t.Fatal("foreign account accepted", err)
	}
}

func TestLegacyIncomingReactionProjectionRefusesUnprovenCoordinates(t *testing.T) {
	s := testStore(t)
	b := prepareIncomingReactionFixture(t, s, legacyInteractionSQL()+legacyValueSQL("wa-reaction-state", legacy.OutgoingReactionKey("fixture-account", "77777@g.us", "unproven-source"), `{"revision":1,"senders":{"888888@lid":{"emoji":"👍","order":1}}}`))
	if _, err := s.ProjectLegacyIncomingReactions(t.Context(), b.Account); !errors.Is(err, legacy.ErrReactionEvidence) {
		t.Fatal("opaque hash invented source coordinates", err)
	}
	var n int
	if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM legacy_incoming_reaction_projections").Scan(&n); err != nil || n != 0 {
		t.Fatal("incomplete evidence committed a checkpoint", err)
	}
}
