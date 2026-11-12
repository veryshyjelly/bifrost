package postgres

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

func TestLegacyCompletionProjectionPreservesExplicitEvidenceWithoutAdoptingEffects(t *testing.T) {
	s := testStore(t)
	b := prepareRouteFixture(t, s, `INSERT INTO kv VALUES('wa-complete','999999:2@lid:original:part','true');
INSERT INTO kv VALUES('wa-complete','12025550123@c.us:original:part','true');
INSERT INTO kv VALUES('wa-complete','12345@g.us:unfinished','false');
INSERT INTO effects VALUES('unknown-send','uncertain',NULL);`)
	if _, err := s.ProjectLegacyRoutes(t.Context(), b.Account, [32]byte{}, ""); err != nil {
		t.Fatal(err)
	}
	result, err := s.ProjectLegacyCompletions(t.Context(), b.Account)
	if err != nil || result.Messages != 1 {
		t.Fatal("explicit completion projection failed", err)
	}
	for _, tc := range []struct {
		account, chat, id string
		expected          bool
	}{
		{b.Account, "12025550123@c.us", "original:part", true},
		{b.Account, "12345@g.us", "unfinished", false},
		{"other-account", "12025550123@c.us", "original:part", false},
	} {
		complete, err := s.MessageComplete(t.Context(), tc.account, tc.chat, tc.id)
		if err != nil || complete != tc.expected {
			t.Fatal("completion evidence escaped account/source scope")
		}
	}
	var effects, jobs int
	_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM effects").Scan(&effects)
	_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs").Scan(&jobs)
	if effects != 0 || jobs != 0 || !errors.Is(s.LegacyRuntimeReady(t.Context(), b.Account), ErrLegacyPending) {
		t.Fatal("completion projection adopted source effects or activated work")
	}
	replay, err := s.ProjectLegacyCompletions(t.Context(), b.Account)
	if err != nil || !reflect.DeepEqual(result, replay) {
		t.Fatal("completion replay changed checkpoint")
	}
}

func TestLegacyCompletionProjectionRollsBackAndRequiresValidSourceKeys(t *testing.T) {
	for _, scenario := range []string{"checkpoint failure", "malformed key", "missing routes"} {
		t.Run(scenario, func(t *testing.T) {
			s := testStore(t)
			key := "12345@g.us:completed"
			if scenario == "malformed key" {
				key = "malformed-source-key"
			}
			b := prepareRouteFixture(t, s, "INSERT INTO kv VALUES('wa-complete','"+key+"','true');")
			if scenario != "missing routes" {
				if _, err := s.ProjectLegacyRoutes(t.Context(), b.Account, [32]byte{}, ""); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "checkpoint failure" {
				_, err := s.pool.Exec(t.Context(), `CREATE FUNCTION reject_completion_checkpoint() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END; $$;
CREATE TRIGGER reject_completion_checkpoint BEFORE INSERT ON legacy_completion_projections FOR EACH ROW EXECUTE FUNCTION reject_completion_checkpoint();`)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.ProjectLegacyCompletions(t.Context(), b.Account); err == nil {
				t.Fatal("incomplete completion projection accepted")
			}
			var count int
			_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM source_completions").Scan(&count)
			if count != 0 {
				t.Fatal("partial completion evidence survived rollback")
			}
		})
	}
}

func recordFixtureCompletion(t *testing.T, s *Store, chat, id string) {
	t.Helper()
	if err := s.queries.RecordSourceCompletion(t.Context(), db.RecordSourceCompletionParams{AccountID: "fixture-account", WhatsappChat: chat, MessageID: id, SourceKey: chat + ":" + id}); err != nil {
		t.Fatal(err)
	}
}

func TestSourceCompletionPreventsNewNativeAndTextPlansWithoutProviderSends(t *testing.T) {
	n := newNativeFixture(t, nil)
	recordFixtureCompletion(t, n.f.store, "peer@g.us", "already-delivered")
	if _, err := n.f.store.pool.Exec(t.Context(), "DELETE FROM bridge_routes WHERE whatsapp_chat='peer@g.us'"); err != nil {
		t.Fatal(err)
	}
	if r := n.incoming(t, "already-delivered", "Do not resend"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := n.f.incoming(t, "new-text-webhook", relay.Incoming{ChatID: "peer@g.us", MessageID: "already-delivered", Body: "Do not resend"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 0 || n.reads != 0 || len(n.sends) != 0 {
		t.Fatal("completed source caused native provider calls")
	}
	var plans int
	_ = n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM relay_plans").Scan(&plans)
	if plans != 0 {
		t.Fatal("completed source materialized a replacement plan")
	}
}

func TestSourceCompletionDoesNotBypassExistingFrozenNativePlan(t *testing.T) {
	n := newNativeFixture(t, nil)
	n.f.route(t, "peer@g.us", -100, 44)
	n.failMethod, n.failCode = "sendPhoto", 429
	if r := n.incoming(t, "frozen-before-completion", strings.Repeat("caption", 200)); r.State != delivery.Pending {
		t.Fatal(r)
	}
	recordFixtureCompletion(t, n.f.store, "peer@g.us", "frozen-before-completion")
	n.mu.Lock()
	n.failMethod = ""
	n.mu.Unlock()
	if _, err := n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE dedup='ow:frozen-before-completion'"); err != nil {
		t.Fatal(err)
	}
	if r := n.f.dispatch(t, "ow:frozen-before-completion", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	plan, found, err := n.f.store.NativePlan(t.Context(), "fixture-account", relay.NativeIncomingKey("peer@g.us", "frozen-before-completion"))
	if err != nil || !found || !plan.Complete {
		t.Fatal("completion marker bypassed frozen partial work")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 3 || n.calls[2].Method != "sendPhoto" || n.reads != 1 {
		t.Fatal("frozen source was repeated or skipped", n.calls, n.reads)
	}
}

func TestSourceCompletionDoesNotSuppressExplicitHistoryAllCopies(t *testing.T) {
	h := newHistoryFixture(t)
	n := newNativeFixture(t, h.f)
	wireNativeHistory(t, h, n)
	recordFixtureCompletion(t, h.f.store, "peer@g.us", "completed-live-source")
	h.batch.Store([]sdk.ChatHistoryMessage{{ID: "completed-live-source", ChatID: "peer@g.us", Author: "12025550123@c.us", Body: "Fresh requested history copy", Type: sdk.MsgText, Timestamp: 1700000000}})
	ids := fetchNativeHistory(t, h, "all")
	if len(ids) != 1 {
		t.Fatal("explicit history request skipped completed source")
	}
	if r := h.f.run(t, ids[0], "history-message", h.service.Item); r.State != delivery.Done {
		t.Fatal(r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	found := false
	for _, call := range n.calls {
		if strings.Contains(call.Text, "Fresh requested history copy") {
			found = true
		}
	}
	if !found {
		t.Fatal("live completion suppressed a scoped history-all copy")
	}
}
