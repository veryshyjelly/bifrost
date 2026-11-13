package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/legacy"
)

func TestLegacyActivationRequiresRetainedProjectionFreshPolicyAndAssetReceipt(t *testing.T) {
	s := testStore(t)
	b, _ := legacyPreparationBundle(t, `INSERT INTO jobs VALUES(1,'old-source','wa:12345@g.us','wa','{}','uncertain',1,0,NULL,1700000000000);`)
	defer b.Close()
	media, err := b.Directory("media")
	if err != nil {
		t.Fatal(err)
	}
	telegram, err := b.Directory("telegram-data")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := legacy.VerifyRestoredAssets(t.Context(), b, media, telegram)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PrepareLegacyImport(t.Context(), b, legacy.SQLiteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = s.ActivateLegacyImport(t.Context(), b, receipt); !errors.Is(err, ErrLegacyPending) {
		t.Fatal("incomplete preparation activated", err)
	}
	if _, err = s.StageLegacyImport(t.Context(), b, legacy.SQLiteOptions{}, "planned-session"); err != nil {
		t.Fatal(err)
	}
	if err = s.ActivateLegacyImport(t.Context(), b, receipt); !errors.Is(err, ErrLegacyPending) {
		t.Fatal("missing queue policy activated", err)
	}
	if err = s.ChooseLegacyFreshQueue(t.Context(), b.Account, b.Digest); err != nil {
		t.Fatal(err)
	}
	invalid := receipt
	invalid.Account = "foreign-account"
	if err = s.ActivateLegacyImport(t.Context(), b, invalid); err == nil {
		t.Fatal("foreign receipt activated")
	}
	if err = s.ActivateLegacyImport(t.Context(), b, receipt); err != nil {
		t.Fatal(err)
	}
	if err = s.LegacyRuntimeReady(t.Context(), b.Account); err != nil {
		t.Fatal("valid activation remained blocked", err)
	}
	configuration, found, err := s.LegacyRuntimeConfiguration(t.Context(), b.Account, b.Configuration().EncryptionKey())
	if err != nil || !found || configuration.MasterID() != 999 || configuration.OwnerID() != 123 {
		t.Fatal("startup configuration not retained", err)
	}
	if accepted, err := s.Enqueue(t.Context(), delivery.Event{AccountID: b.Account, Dedup: "old-source", Stream: "wa:12345@g.us", Kind: "wa", Payload: []byte(`{}`)}); err != nil || accepted {
		t.Fatal("activation replayed old ingress", err)
	}
	if accepted, err := s.Enqueue(t.Context(), delivery.Event{AccountID: b.Account, Dedup: "new-history-run", Stream: "history:new-run", Kind: "history", Payload: []byte(`{}`)}); err != nil || !accepted {
		t.Fatal("fresh history blocked", err)
	}
	job, err := s.Claim(t.Context(), time.Minute)
	if err != nil || job.Dedup != "new-history-run" {
		t.Fatal("activation did not release native claims", err)
	}
	finish(t, s, job)
	if err = s.ActivateLegacyImport(t.Context(), b, receipt); err != nil {
		t.Fatal("exact activation replay changed state", err)
	}
	invalid = receipt
	invalid.Media.Bytes++
	if err = s.ActivateLegacyImport(t.Context(), b, invalid); !errors.Is(err, ErrLegacyDestination) {
		t.Fatal("changed activation evidence accepted", err)
	}
	var archived int
	if err = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM legacy_rows WHERE source_table='jobs'").Scan(&archived); err != nil || archived != 1 {
		t.Fatal("activation lost original uncertainty", err)
	}
	if _, err = s.pool.Exec(t.Context(), "UPDATE legacy_asset_verifications SET receipt='{}'"); err == nil {
		t.Fatal("asset receipt mutable")
	}
}

func TestLegacyActivationNeverDiscardsPreviouslyAdmittedNativeWork(t *testing.T) {
	s := testStore(t)
	b, _ := legacyPreparationBundle(t, "")
	defer b.Close()
	if _, err := s.StageLegacyImport(t.Context(), b, legacy.SQLiteOptions{}, "planned-session"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChooseLegacyFreshQueue(t.Context(), b.Account, b.Digest); err != nil {
		t.Fatal(err)
	}
	media, _ := b.Directory("media")
	telegram, _ := b.Directory("telegram-data")
	receipt, err := legacy.VerifyRestoredAssets(t.Context(), b, media, telegram)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Enqueue(t.Context(), delivery.Event{AccountID: b.Account, Dedup: "native", Stream: "wa:12345@g.us", Kind: "wa", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err = s.ActivateLegacyImport(t.Context(), b, receipt); !errors.Is(err, ErrLegacyDestination) {
		t.Fatal("populated queue silently discarded", err)
	}
	var jobs, receipts int
	if err = s.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM jobs),(SELECT count(*) FROM legacy_asset_verifications)").Scan(&jobs, &receipts); err != nil || jobs != 1 || receipts != 0 {
		t.Fatal("activation refusal partially committed", err)
	}
	if !errors.Is(s.LegacyRuntimeReady(t.Context(), b.Account), ErrLegacyPending) {
		t.Fatal("failed activation opened claims")
	}
}
