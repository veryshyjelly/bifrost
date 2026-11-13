package postgres

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/legacy"
)

func TestLegacyStagingResumesCapabilitiesAndKeepsDeliveryInactive(t *testing.T) {
	s := testStore(t)
	b, _ := legacyPreparationBundle(t, "")
	defer b.Close()
	_, err := s.pool.Exec(t.Context(), `CREATE FUNCTION reject_stage() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private fixture detail'; END; $$;
CREATE TRIGGER reject_stage BEFORE INSERT ON legacy_change_projections FOR EACH ROW EXECUTE FUNCTION reject_stage();`)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := s.StageLegacyImport(t.Context(), b, legacy.SQLiteOptions{}, "planned-session")
	var stage *LegacyStageError
	if !errors.As(err, &stage) || stage.Stage != "changes" || strings.Contains(err.Error(), "private fixture detail") || len(partial.Completed) != 6 || partial.Ready {
		t.Fatal("failure lost its resumable boundary or exposed private cause", err)
	}
	if !errors.Is(s.LegacyRuntimeReady(t.Context(), b.Account), ErrLegacyPending) {
		t.Fatal("partial staging enabled delivery")
	}
	if _, err = s.pool.Exec(t.Context(), "DROP TRIGGER reject_stage ON legacy_change_projections"); err != nil {
		t.Fatal(err)
	}
	result, err := s.StageLegacyImport(t.Context(), b, legacy.SQLiteOptions{}, "planned-session")
	if err != nil || result.Ready || result.Preparation.Ready || len(result.Completed) != 9 {
		t.Fatal("staging did not resume all implemented capabilities", err)
	}
	replay, err := s.StageLegacyImport(t.Context(), b, legacy.SQLiteOptions{}, "planned-session")
	if err != nil || !reflect.DeepEqual(result, replay) {
		t.Fatal("exact archive replay changed staging", err)
	}
	var jobs, effects int
	if err = s.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM jobs),(SELECT count(*) FROM effects)").Scan(&jobs, &effects); err != nil || jobs != 0 || effects != 0 {
		t.Fatal("staging created runnable work or fake effects", err)
	}
	if !errors.Is(s.LegacyRuntimeReady(t.Context(), b.Account), ErrLegacyPending) {
		t.Fatal("completed preparation activated unfinished import")
	}
	if _, err = s.StageLegacyImport(t.Context(), b, legacy.SQLiteOptions{}, "different-session"); err == nil {
		t.Fatal("resumed staging silently changed planned connection")
	}
}
