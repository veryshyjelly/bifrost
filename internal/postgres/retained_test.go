package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/retained"
	"github.com/veryshyjelly/bifrost/internal/secrets"
)

// The SQL identifiers below belong to applied migration history. Fixtures write
// only isolated test schemas; the runtime retains read-only compatibility.
func seedRetainedAccount(t *testing.T, s *Store, ciphertext string, activated bool) {
	t.Helper()
	_, err := s.pool.Exec(t.Context(), `INSERT INTO legacy_imports(account_id,archive_digest,sqlite_digest,manifest,configuration_ciphertext,report,activated_at)
VALUES('fixture-account',$1,$1,'{}',$2,'{}',CASE WHEN $3 THEN now() ELSE NULL END)`, strings.Repeat("a", 64), ciphertext, activated)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRetainedActivationAndDeduplicationRemainFenced(t *testing.T) {
	s := testStore(t)
	if err := s.RetainedStateReady(t.Context(), "fixture-account"); err != nil {
		t.Fatal(err)
	}
	enqueue(t, s, "current-job", "current-stream")
	seedRetainedAccount(t, s, "opaque-configuration", false)
	if err := s.RetainedStateReady(t.Context(), "fixture-account"); !errors.Is(err, ErrRetainedPending) {
		t.Fatal(err)
	}
	if _, err := s.AcquireRuntimeLease(t.Context(), "fixture-account"); !errors.Is(err, ErrRetainedPending) {
		t.Fatal("runtime ownership bypassed activation", err)
	}
	if _, err := s.Claim(t.Context(), time.Minute); err == nil {
		t.Fatal("ordinary claim bypassed activation")
	}
	if _, err := s.pool.Exec(t.Context(), "UPDATE legacy_imports SET activated_at=now() WHERE account_id='fixture-account'"); err != nil {
		t.Fatal(err)
	}
	if err := s.RetainedStateReady(t.Context(), "fixture-account"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(t.Context(), "INSERT INTO legacy_ingress_dedup(account_id,dedup) VALUES('fixture-account','retained-event')"); err != nil {
		t.Fatal(err)
	}
	accepted, err := s.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: "retained-event", Stream: "retained", Kind: "fixture", Payload: json.RawMessage(`{}`)})
	if err != nil || accepted {
		t.Fatal("retained event resurrected", accepted, err)
	}
	if _, err := s.Claim(t.Context(), time.Minute); err != nil {
		t.Fatal("current job not released", err)
	}
	if _, err := s.pool.Exec(t.Context(), "DELETE FROM legacy_ingress_dedup WHERE account_id='fixture-account'"); err == nil {
		t.Fatal("immutable dedup evidence became mutable")
	}
}

func TestRetainedRuntimeConfigurationKeepsCipherAndAccountScope(t *testing.T) {
	s := testStore(t)
	api := "TELEGRAM_API_ID=12345\nTELEGRAM_API_HASH=" + strings.Repeat("ab", 16) + "\n"
	bridge := api + "BIFROST_ACCOUNT_ID=fixture-account\nBRIDGE_ENCRYPTION_KEY=" + strings.Repeat("00", 32) + "\nTELEGRAM_OWNER_ID=101\nTELEGRAM_MANAGEMENT_CHAT_ID=-100\nTELEGRAM_MASTER_TOKEN=11:" + strings.Repeat("x", 32) + "\nBRIDGE_ADMIN_TOKEN=" + strings.Repeat("admin", 8) + "\nOPENWA_WEBHOOK_SECRET=" + strings.Repeat("hook", 8) + "\n"
	raw, _ := json.Marshal(struct{ Bridge, Telegram []byte }{[]byte(bridge), []byte(api)})
	cipher, _ := secrets.New([32]byte{})
	sealed, err := cipher.Seal(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	seedRetainedAccount(t, s, sealed, true)
	c, found, err := s.RetainedRuntimeConfiguration(t.Context(), "fixture-account", [32]byte{})
	if err != nil || !found || c.MasterID() != 11 || c.OwnerID() != 101 || c.ManagementChatID() != -100 {
		t.Fatal("retained identity changed", found, err)
	}
	wrong := [32]byte{1}
	if _, _, err := s.RetainedRuntimeConfiguration(t.Context(), "fixture-account", wrong); !errors.Is(err, retained.ErrInvalid) {
		t.Fatal("changed encryption key accepted", err)
	}
	if _, found, err := s.RetainedRuntimeConfiguration(t.Context(), "other-account", [32]byte{}); err != nil || found {
		t.Fatal("account scope escaped", found, err)
	}
	var actual string
	if err := s.pool.QueryRow(t.Context(), "SELECT configuration_ciphertext FROM legacy_imports WHERE account_id='fixture-account'").Scan(&actual); err != nil || actual != sealed {
		t.Fatal("original ciphertext rewritten", err)
	}
}
