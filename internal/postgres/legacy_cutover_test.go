package postgres

import (
	"errors"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/legacy"
)

func TestFreshQueuePolicyArchivesOriginalWorkWithoutResolvingOrActivatingIt(t *testing.T) {
	s := testStore(t)
	b, _ := legacyPreparationBundle(t, `INSERT INTO jobs VALUES(1,'original','wa:12345@g.us','wa','{}','uncertain',1,0,NULL,1700000000000); INSERT INTO effects VALUES('original-send','uncertain',NULL);`)
	defer b.Close()
	if _, err := s.StageLegacyImport(t.Context(), b, legacy.SQLiteOptions{}, "planned-session"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChooseLegacyFreshQueue(t.Context(), b.Account, b.Digest); err != nil {
		t.Fatal(err)
	}
	if err := s.ChooseLegacyFreshQueue(t.Context(), b.Account, b.Digest); err != nil {
		t.Fatal("policy replay changed state", err)
	}
	staged, err := s.StageLegacyImport(t.Context(), b, legacy.SQLiteOptions{}, "planned-session")
	if err != nil || staged.QueueMode != "fresh" || staged.Ready {
		t.Fatal("rehearsal did not report the recorded policy", err)
	}
	var nativeJobs, nativeEffects, sourceUncertain int
	err = s.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM jobs),(SELECT count(*) FROM effects),(SELECT count(*) FROM legacy_rows WHERE source_table IN ('jobs','effects') AND convert_from(raw_row,'UTF8')::jsonb->>'state'='uncertain')`).Scan(&nativeJobs, &nativeEffects, &sourceUncertain)
	if err != nil || nativeJobs != 0 || nativeEffects != 0 || sourceUncertain != 2 {
		t.Fatal("source uncertainty was replayed, dropped or marked complete", err)
	}
	if !errors.Is(s.LegacyRuntimeReady(t.Context(), b.Account), ErrLegacyPending) {
		t.Fatal("queue policy activated unfinished import")
	}
	if accepted, err := s.Enqueue(t.Context(), delivery.Event{AccountID: b.Account, Dedup: "original", Stream: "wa:12345@g.us", Kind: "wa", Payload: []byte(`{}`)}); err != nil || accepted {
		t.Fatal("archived backlog was re-enqueued by duplicate ingress", err)
	}
	if err := s.ChooseLegacyFreshQueue(t.Context(), b.Account, "different-archive"); !errors.Is(err, ErrLegacyDestination) {
		t.Fatal("policy changed archive", err)
	}
}
