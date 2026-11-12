package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/bots"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/secrets"
)

func TestLegacyBotProjectionPreservesCredentialsAndNeverEstablishesLocalEligibility(t *testing.T) {
	s := testStore(t)
	b, sealed := legacyPreparationBundle(t, `INSERT INTO kv VALUES('migrated','master','true'); INSERT INTO kv VALUES('migrated','77','true');
INSERT INTO kv VALUES('telegram-identity','77','{"id":77,"is_bot":true,"first_name":"Cached","username":"cached_name"}');`)
	if _, err := s.PrepareLegacyImport(t.Context(), b, legacy.SQLiteOptions{}); err != nil {
		t.Fatal(err)
	}
	result, err := s.ProjectLegacyBots(t.Context(), b.Account, [32]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Credentials != 2 || result.HostedLogouts != 2 || result.ArchiveDigest != b.Digest {
		t.Fatal("incomplete credential projection")
	}
	credential, found, err := s.Credential(t.Context(), b.Account, "77")
	if err != nil || !found || credential.Ciphertext != sealed || credential.Revision != bots.Digest("77:original-fixture-token") || credential.Identity.Username != "fixture_bot" || !credential.CreatedNotice {
		t.Fatal("original credential, name or notice state changed")
	}
	var started bool
	if err := s.pool.QueryRow(t.Context(), "SELECT started FROM bridge_bots WHERE account_id=$1 AND bot_id='77'", b.Account).Scan(&started); err != nil || !started {
		t.Fatal("original Start activation lost")
	}
	var eligible, setups, jobs int
	_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM telegram_bot_lifecycle").Scan(&eligible)
	_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM telegram_setup_plans").Scan(&setups)
	_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs").Scan(&jobs)
	if eligible != 0 || setups != 0 || jobs != 0 || !errors.Is(s.LegacyRuntimeReady(t.Context(), b.Account), ErrLegacyPending) {
		t.Fatal("credential projection activated provider work or migration eligibility")
	}
	for _, proof := range []db.LegacyBotLogoutParams{
		{AccountID: b.Account, BotID: "77", CredentialRevision: credential.Revision},
		{AccountID: "another-account", BotID: "77", CredentialRevision: credential.Revision},
		{AccountID: b.Account, BotID: "88", CredentialRevision: credential.Revision},
		{AccountID: b.Account, BotID: "77", CredentialRevision: "another-revision"},
	} {
		got, err := s.queries.LegacyBotLogout(t.Context(), proof)
		expected := proof.AccountID == b.Account && proof.BotID == "77" && proof.CredentialRevision == credential.Revision
		if err != nil || got != expected {
			t.Fatal("logout proof escaped account/bot/credential scope")
		}
	}
	// Replaying a capability projection never restores an old token over a
	// subsequent credential change. Its first result remains immutable.
	cipher, _ := secrets.New([32]byte{})
	rotated, _ := cipher.Seal("77:rotated-fixture-token")
	if _, err := s.pool.Exec(t.Context(), "UPDATE telegram_credentials SET ciphertext=$1,revision=$2 WHERE bot_id='77'", rotated, bots.Digest("77:rotated-fixture-token")); err != nil {
		t.Fatal(err)
	}
	replay, err := s.ProjectLegacyBots(t.Context(), b.Account, [32]byte{})
	after, _, readErr := s.Credential(t.Context(), b.Account, "77")
	if err != nil || readErr != nil || !reflect.DeepEqual(result, replay) || after.Ciphertext != rotated {
		t.Fatal("projection replay reset subsequent credential state")
	}
	if _, err := s.ProjectLegacyBots(t.Context(), b.Account, [32]byte{1}); !errors.Is(err, ErrLegacyDestination) {
		t.Fatal("wrong original key accepted")
	}
}

func TestLegacyBotProjectionRollbackAndConflictingNativeCredentials(t *testing.T) {
	for _, scenario := range []string{"database rollback", "existing native credential"} {
		t.Run(scenario, func(t *testing.T) {
			s := testStore(t)
			b, _ := legacyPreparationBundle(t, `INSERT INTO kv VALUES('migrated','77','true');`)
			if _, err := s.PrepareLegacyImport(t.Context(), b, legacy.SQLiteOptions{}); err != nil {
				t.Fatal(err)
			}
			if scenario == "database rollback" {
				_, err := s.pool.Exec(t.Context(), `CREATE FUNCTION reject_logout_proof() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END; $$;
CREATE TRIGGER reject_logout_proof BEFORE INSERT ON legacy_bot_logouts FOR EACH ROW EXECUTE FUNCTION reject_logout_proof();`)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.ProjectLegacyBots(t.Context(), b.Account, [32]byte{}); err == nil {
					t.Fatal("failed logout-proof projection committed")
				}
				var credentials, markers int
				_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM telegram_credentials").Scan(&credentials)
				_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM legacy_bot_projections").Scan(&markers)
				if credentials != 0 || markers != 0 {
					t.Fatal("partial credential projection survived rollback")
				}
				_, err = s.pool.Exec(t.Context(), "DROP TRIGGER reject_logout_proof ON legacy_bot_logouts")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.ProjectLegacyBots(t.Context(), b.Account, [32]byte{}); err != nil {
					t.Fatal(err)
				}
			} else {
				_, err := s.pool.Exec(t.Context(), `INSERT INTO telegram_credentials(account_id,bot_id,ciphertext,revision,identity) VALUES('fixture-account','master','existing-ciphertext','existing-revision','{"id":999,"is_bot":true}')`)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.ProjectLegacyBots(t.Context(), b.Account, [32]byte{}); !errors.Is(err, ErrLegacyDestination) {
					t.Fatal("native credentials merged into projection")
				}
			}
		})
	}
}

func TestLegacyBotNativeSetupUsesOnlyScopedLogoutProofAndStillChecksLocalAPI(t *testing.T) {
	for _, scenario := range []string{"confirmed legacy logout", "Start and cache alone", "rotated credential", "local API refusal"} {
		t.Run(scenario, func(t *testing.T) {
			s := testStore(t)
			source := `INSERT INTO kv VALUES('telegram-identity','77','{"id":77,"is_bot":true,"username":"cached_name"}');`
			if scenario != "Start and cache alone" {
				source += `INSERT INTO kv VALUES('migrated','77','true');`
			}
			b, _ := legacyPreparationBundle(t, source)
			if _, err := s.PrepareLegacyImport(t.Context(), b, legacy.SQLiteOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ProjectLegacyBots(t.Context(), b.Account, [32]byte{}); err != nil {
				t.Fatal(err)
			}
			if scenario == "rotated credential" {
				cipher, _ := secrets.New([32]byte{})
				sealed, _ := cipher.Seal("77:rotated-fixture-token")
				if _, err := s.pool.Exec(t.Context(), "UPDATE telegram_credentials SET ciphertext=$1,revision=$2 WHERE bot_id='77'", sealed, bots.Digest("77:rotated-fixture-token")); err != nil {
					t.Fatal(err)
				}
			}
			// This isolated fixture contains only credentials. Simulate completed
			// activation of its unrelated import stages to exercise native setup;
			// production preparation/projection APIs never set this marker.
			if _, err := s.pool.Exec(t.Context(), "UPDATE legacy_imports SET activated_at=now()"); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			calls := map[string]int{}
			refuseMe := scenario == "local API refusal"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				calls[method]++
				var parseErr error
				if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
					parseErr = r.ParseMultipartForm(1 << 20)
				} else {
					parseErr = r.ParseForm()
				}
				if parseErr != nil {
					t.Error(parseErr)
				}
				switch method {
				case "logout":
					fmt.Fprint(w, `{"ok":true,"result":true}`)
				case "getMe":
					if refuseMe {
						fmt.Fprint(w, `{"ok":false,"error_code":403,"description":"Forbidden"}`)
						return
					}
					fmt.Fprint(w, `{"ok":true,"result":{"id":77,"is_bot":true,"first_name":"Native","username":"native_bot"}}`)
				case "setWebhook":
					if r.FormValue("drop_pending_updates") == "true" || r.FormValue("secret_token") != ingress.TelegramSecret("77", strings.Repeat("a", 32)) {
						t.Error("original secret or pending updates changed")
					}
					fmt.Fprint(w, `{"ok":true,"result":true}`)
				default:
					t.Error("unexpected provider operation", method)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			pool, err := bots.NewPool(s, [32]byte{}, b.Account, server.URL, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			local := func(ctx context.Context, account, id string) (bots.Telegram, error) {
				return pool.Local(ctx, account, id)
			}
			cloud := func(ctx context.Context, account, id string) (bots.Telegram, error) {
				return pool.Cloud(ctx, account, id)
			}
			settings := bots.Settings{Session: "new-provider-connection", Callback: "http://bifrost:3210", OwnerID: 123, Management: -100, CommandTTL: 60, ControlTTL: 600}
			settings.Configuration = bots.Digest([]string{settings.Callback, ingress.TelegramSecret("master", strings.Repeat("a", 32))})
			service, err := bots.New(s, local, cloud, bots.Options{Settings: settings, AdminToken: strings.Repeat("a", 32)})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.QueueBotSetups(t.Context(), b.Account, settings); err != nil {
				t.Fatal(err)
			}
			var id int64
			if err := s.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='control-bot-setup' AND payload->>'bot'='77'").Scan(&id); err != nil {
				t.Fatal(err)
			}
			f := &relayFixture{store: s}
			report := f.run(t, id, "control-bot-setup", service.Setup)
			if scenario == "local API refusal" {
				if report.State != delivery.Failed {
					t.Fatal(report)
				}
				var eligible int
				_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM telegram_bot_lifecycle").Scan(&eligible)
				if eligible != 0 {
					t.Fatal("refused local API established migration eligibility")
				}
				mu.Lock()
				refuseMe = false
				mu.Unlock()
				if err := s.Review(t.Context(), id, true, false); err != nil {
					t.Fatal(err)
				}
				report = f.run(t, id, "control-bot-setup", service.Setup)
			}
			if report.State != delivery.Done {
				t.Fatal(report)
			}
			mu.Lock()
			defer mu.Unlock()
			logout := 0
			if scenario == "Start and cache alone" || scenario == "rotated credential" {
				logout = 1
			}
			me := 1
			if scenario == "local API refusal" {
				me = 2
			}
			if calls["logout"] != logout || calls["getMe"] != me || calls["setWebhook"] != 1 {
				t.Fatalf("incorrect native setup sequence: %v", calls)
			}
			var identity []byte
			if err := s.pool.QueryRow(t.Context(), "SELECT identity FROM telegram_bot_lifecycle WHERE bot_id='77'").Scan(&identity); err != nil || !json.Valid(identity) || !bytes.Contains(identity, []byte("native_bot")) {
				t.Fatal("native local identity receipt missing")
			}
		})
	}
}
