package postgres

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/secrets"
)

func legacyPreparationBundle(t *testing.T, extra string) (*legacy.Bundle, string) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python sqlite3 required")
	}
	cipher, _ := secrets.New([32]byte{})
	sealed, _ := cipher.Seal("77:original-fixture-token")
	savedBot, _ := json.Marshal(map[string]any{"token": sealed, "username": "fixture_bot", "ready": true})
	schema := `
CREATE TABLE kv(kind TEXT,id TEXT,value TEXT NOT NULL,PRIMARY KEY(kind,id));
CREATE TABLE jobs(id INTEGER PRIMARY KEY,dedup TEXT UNIQUE NOT NULL,stream TEXT NOT NULL,type TEXT NOT NULL,payload TEXT NOT NULL,state TEXT NOT NULL DEFAULT 'pending',attempts INTEGER NOT NULL DEFAULT 0,due INTEGER NOT NULL DEFAULT 0,error TEXT,created INTEGER NOT NULL);
CREATE TABLE effects(id TEXT PRIMARY KEY,state TEXT NOT NULL,result TEXT);
CREATE TABLE messages(wa_chat TEXT NOT NULL,wa_id TEXT NOT NULL,tg_chat TEXT NOT NULL,tg_id INTEGER NOT NULL,bot TEXT NOT NULL,direction TEXT NOT NULL,media INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(bot,tg_chat,tg_id));
CREATE TABLE aliases(alias TEXT PRIMARY KEY,canonical TEXT NOT NULL);
CREATE TABLE telegram_messages(seq INTEGER PRIMARY KEY,scope TEXT NOT NULL,chat TEXT NOT NULL,message INTEGER NOT NULL,bot TEXT NOT NULL,thread INTEGER NOT NULL DEFAULT 0,kind TEXT NOT NULL,date INTEGER NOT NULL,expires INTEGER NOT NULL DEFAULT 0,deleted INTEGER NOT NULL DEFAULT 0,error TEXT,UNIQUE(scope,chat,message));
INSERT INTO kv VALUES('identity','account','{"id":"fixture-account"}');
INSERT INTO kv VALUES('bot','77','` + string(savedBot) + `');
` + extra
	database := filepath.Join(t.TempDir(), "bridge.db")
	command := exec.CommandContext(t.Context(), python, "-I", "-S", "-c", "import sqlite3,sys; c=sqlite3.connect(sys.argv[1]); c.executescript(sys.stdin.read()); c.close()", database)
	command.Stdin = strings.NewReader(schema)
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		t.Fatal("could not create private legacy fixture")
	}
	data, err := os.ReadFile(database)
	if err != nil {
		t.Fatal(err)
	}
	archive := func(files map[string][]byte) []byte {
		var buffer bytes.Buffer
		z := gzip.NewWriter(&buffer)
		tw := tar.NewWriter(z)
		if err := tw.WriteHeader(&tar.Header{Name: ".", Typeflag: tar.TypeDir, Mode: 0700}); err != nil {
			t.Fatal(err)
		}
		for name, body := range files {
			if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(body))}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(body); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes()
	}
	files := map[string][]byte{
		"bridge.env":           []byte("BRIDGE_ENCRYPTION_KEY=" + strings.Repeat("0", 64) + "\nBRIDGE_ADMIN_TOKEN=" + strings.Repeat("a", 32) + "\nOPENWA_WEBHOOK_SECRET=" + strings.Repeat("w", 32) + "\nTELEGRAM_MASTER_TOKEN=999:fixture-master-token\nTELEGRAM_OWNER_ID=123\nTELEGRAM_MANAGEMENT_CHAT_ID=-100\nTELEGRAM_API_ID=12345\nTELEGRAM_API_HASH=" + strings.Repeat("a", 32) + "\nBIFROST_ACCOUNT_ID=fixture-account\nOPENWA_SESSION_ID=source-session\n"),
		"telegram-api.env":     []byte("TELEGRAM_API_ID=12345\nTELEGRAM_API_HASH=" + strings.Repeat("a", 32) + "\n"),
		"data.tar.gz":          archive(map[string][]byte{"bridge.db": data}),
		"media.tar.gz":         archive(map[string][]byte{"frozen.webp": []byte("original frozen bytes")}),
		"telegram-data.tar.gz": archive(map[string][]byte{}),
	}
	m := legacy.Manifest{Format: "bifrost", Version: 1, Created: time.Now().UTC(), Source: legacy.ManifestSource{Project: "fixture", Revision: "checkpoint-typescript-baseline"}, Files: map[string]string{}}
	for name, body := range files {
		sum := sha256.Sum256(body)
		m.Files[name] = hex.EncodeToString(sum[:])
	}
	files["manifest.json"], _ = json.Marshal(m)
	file := filepath.Join(t.TempDir(), "export.tar.gz")
	if err := os.WriteFile(file, archive(files), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := legacy.OpenBundle(t.Context(), file, legacy.BundleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b, sealed
}

func TestLegacyPreparationPreservesEvidenceAndBlocksDispatch(t *testing.T) {
	s := testStore(t)
	b, sealed := legacyPreparationBundle(t, `
INSERT INTO jobs VALUES(9007199254740993,'original-send-key','wa:peer','wa','{"accountId":"fixture-account","sessionId":"source-session"}','running',3,1700000000000,'source error',1700000000000);
INSERT INTO effects VALUES('original-send-key:to:master:-100:text:0','sending',NULL);
INSERT INTO effects VALUES('completed-send-key','done','{"message_id":42}');
INSERT INTO aliases VALUES('999999@lid','12025550123@c.us');
INSERT INTO messages VALUES('12025550123@c.us','source-id','-100',42,'master','in',0);
INSERT INTO telegram_messages VALUES(9007199254740993,'*','-100',42,'master',7,'chat',1700000000000,0,0,NULL);
`)
	result, err := s.PrepareLegacyImport(t.Context(), b, legacy.SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Ready || result.Account != "fixture-account" || result.Snapshot.JobStates["running"] != 1 || result.Snapshot.EffectStates["sending"] != 1 || result.Snapshot.Rows["messages"] != 1 {
		t.Fatal("source evidence relabeled as runnable import")
	}
	var raw []byte
	if err := s.pool.QueryRow(t.Context(), "SELECT raw_row FROM legacy_rows WHERE account_id=$1 AND source_table='jobs'", b.Account).Scan(&raw); err != nil || !bytes.Contains(raw, []byte("9007199254740993")) || !bytes.Contains(raw, []byte(`"state":"running"`)) {
		t.Fatal("original job ID or state changed")
	}
	value, err := s.queries.LegacyValue(t.Context(), db.LegacyValueParams{AccountID: b.Account, Kind: "bot", SourceID: "77"})
	if err != nil || !strings.Contains(value, sealed) {
		t.Fatal("original encrypted token changed")
	}
	checkpoint, err := s.queries.LegacyImport(t.Context(), b.Account)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(checkpoint.ConfigurationCiphertext, "fixture-master-token") || checkpoint.ActivatedAt.Valid {
		t.Fatal("private configuration plaintext or active checkpoint")
	}
	cipher, _ := secrets.New([32]byte{})
	plain, err := cipher.Open(checkpoint.ConfigurationCiphertext)
	var files struct{ Bridge, Telegram []byte }
	if err != nil || json.Unmarshal([]byte(plain), &files) != nil {
		t.Fatal("original private files unrecoverable")
	}
	bridge, telegram := b.Configuration().Files()
	if !bytes.Equal(bridge, files.Bridge) || !bytes.Equal(telegram, files.Telegram) {
		t.Fatal("original configuration bytes changed")
	}
	if !errors.Is(s.LegacyRuntimeReady(t.Context(), b.Account), ErrLegacyPending) {
		t.Fatal("incomplete import passed startup gate")
	}
	var jobs, effects int
	_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs").Scan(&jobs)
	_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM effects").Scan(&effects)
	if jobs != 0 || effects != 0 {
		t.Fatal("source effects adopted or source jobs activated during preparation")
	}
	enqueue(t, s, "later-native-ingress", "later-stream")
	if _, err := s.Claim(t.Context(), time.Minute); !errors.Is(err, delivery.ErrNoJob) {
		t.Fatal("prepared import dispatched work")
	}
	replay, err := s.PrepareLegacyImport(t.Context(), b, legacy.SQLiteOptions{})
	if err != nil || !reflect.DeepEqual(result, replay) {
		t.Fatal("exact replay changed checkpoint")
	}
	if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs").Scan(&jobs); err != nil || jobs != 1 {
		t.Fatal("replay reset subsequent work")
	}
	if _, err := s.pool.Exec(t.Context(), "UPDATE legacy_rows SET raw_row='changed'::bytea"); err == nil {
		t.Fatal("source evidence mutable")
	}
	if _, err := s.pool.Exec(t.Context(), "DELETE FROM legacy_values"); err == nil {
		t.Fatal("source KV evidence deletable")
	}
	if _, err := s.pool.Exec(t.Context(), "UPDATE legacy_imports SET archive_digest=repeat('0',64)"); err == nil {
		t.Fatal("source checkpoint identity mutable")
	}
}

func TestLegacyPreparationRejectsDifferentArchiveNonemptyStoreAndIdentityConflicts(t *testing.T) {
	for _, scenario := range []string{"nonempty", "metadata-cache-only", "account", "key", "different-archive"} {
		t.Run(scenario, func(t *testing.T) {
			s := testStore(t)
			b, _ := legacyPreparationBundle(t, "")
			switch scenario {
			case "nonempty":
				enqueue(t, s, "existing-work", "existing-stream")
			case "metadata-cache-only":
				_, err := s.pool.Exec(t.Context(), "INSERT INTO contact_profiles(account_id,person_id,name) VALUES('fixture-account','12025550123@c.us','Existing profile')")
				if err != nil {
					t.Fatal(err)
				}
			case "account":
				_, err := s.pool.Exec(t.Context(), "UPDATE bridge_identity SET account_id='different-account'")
				if err != nil {
					t.Fatal(err)
				}
			case "key":
				_, err := s.pool.Exec(t.Context(), "UPDATE bridge_identity SET key_fingerprint='different-key'")
				if err != nil {
					t.Fatal(err)
				}
			case "different-archive":
				if _, err := s.PrepareLegacyImport(t.Context(), b, legacy.SQLiteOptions{}); err != nil {
					t.Fatal(err)
				}
				b, _ = legacyPreparationBundle(t, "")
			}
			if _, err := s.PrepareLegacyImport(t.Context(), b, legacy.SQLiteOptions{}); !errors.Is(err, ErrLegacyDestination) {
				t.Fatalf("conflicting destination accepted: %v", err)
			}
		})
	}
}

func TestLegacyPreparationRefusesConcurrentDestinationWriter(t *testing.T) {
	s := testStore(t)
	b, _ := legacyPreparationBundle(t, "")
	writer, err := s.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(writer)
	if _, err = writer.Exec(t.Context(), "INSERT INTO bridge_bots(account_id,bot_id) VALUES('fixture-account','77')"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if _, err = s.PrepareLegacyImport(ctx, b, legacy.SQLiteOptions{}); !errors.Is(err, ErrLegacyDestination) {
		t.Fatalf("concurrent writer was not refused without waiting: %v", err)
	}
}

func TestLegacyPreparationRollsBackCopiedRowsAndNewIdentityOnLateValidationFailure(t *testing.T) {
	s := testStore(t)
	if _, err := s.pool.Exec(t.Context(), "DELETE FROM bridge_identity"); err != nil {
		t.Fatal(err)
	}
	b, _ := legacyPreparationBundle(t, `WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<600) INSERT INTO kv SELECT 'wa-complete','source-'||x,'true' FROM n;
INSERT INTO jobs VALUES(1,'unsupported-key','stream','unsupported','{}','pending',0,0,NULL,1);`)
	if _, err := s.PrepareLegacyImport(t.Context(), b, legacy.SQLiteOptions{}); !errors.Is(err, legacy.ErrUnsupported) {
		t.Fatalf("late source rejection not preserved: %v", err)
	}
	var rows, imports, identities int
	_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM legacy_rows").Scan(&rows)
	_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM legacy_imports").Scan(&imports)
	_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM bridge_identity").Scan(&identities)
	if rows != 0 || imports != 0 || identities != 0 {
		t.Fatal("partial import or new identity survived rollback")
	}
}

func TestLegacyPreparationDatabaseFailureAndConcurrentReplay(t *testing.T) {
	t.Run("database rollback", func(t *testing.T) {
		s := testStore(t)
		b, _ := legacyPreparationBundle(t, "")
		_, err := s.pool.Exec(t.Context(), `CREATE FUNCTION reject_legacy_projection() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture rejection'; END; $$;
CREATE TRIGGER reject_legacy_projection BEFORE INSERT ON legacy_values FOR EACH ROW EXECUTE FUNCTION reject_legacy_projection();`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.PrepareLegacyImport(t.Context(), b, legacy.SQLiteOptions{}); err == nil {
			t.Fatal("failed projection committed")
		}
		var count int
		_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM legacy_rows").Scan(&count)
		if count != 0 {
			t.Fatal("copied source rows survived failed transaction")
		}
		_, err = s.pool.Exec(t.Context(), "DROP TRIGGER reject_legacy_projection ON legacy_values")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.PrepareLegacyImport(t.Context(), b, legacy.SQLiteOptions{}); err != nil {
			t.Fatal("rolled back import could not retry")
		}
	})
	t.Run("concurrent exact replay", func(t *testing.T) {
		s := testStore(t)
		b, _ := legacyPreparationBundle(t, "")
		var wg sync.WaitGroup
		results := make(chan LegacyPreparation, 2)
		failures := make(chan error, 2)
		for range 2 {
			wg.Go(func() {
				r, err := s.PrepareLegacyImport(context.Background(), b, legacy.SQLiteOptions{})
				results <- r
				failures <- err
			})
		}
		wg.Wait()
		close(results)
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		var first *LegacyPreparation
		for r := range results {
			if first == nil {
				copy := r
				first = &copy
			} else if !reflect.DeepEqual(*first, r) {
				t.Fatal("concurrent replay changed result")
			}
		}
		var count int
		_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM legacy_imports").Scan(&count)
		if count != 1 {
			t.Fatal("concurrent replay created multiple checkpoints")
		}
	})
}
