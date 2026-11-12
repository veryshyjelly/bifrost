package postgres

import (
	"encoding/json"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/metadata"
)

func automaticMetadata(t *testing.T, r *runFixture, key, kind, source, scope, id string) delivery.Report {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"type": "sync_metadata", "data": map[string]string{"source": source, "scope": scope, "id": id}})
	if _, err := r.f.store.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: key, Kind: kind, Stream: "metadata-request:" + key, Payload: raw}); err != nil {
		t.Fatal(err)
	}
	var job int64
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", key).Scan(&job); err != nil {
		t.Fatal(err)
	}
	return r.f.run(t, job, kind, r.coordinator.Admit)
}

func TestMetadataSourcesAutomaticRunReportsToManagementAndManualWaiter(t *testing.T) {
	r := newRunFixture(t)
	r.f.route(t, "123@g.us", -200, 0)
	r.url = nil
	if result := automaticMetadata(t, r, "automatic-notifier", "sync-request", "new", "group", "123@g.us"); result.State != delivery.Done {
		t.Fatal(result)
	}
	raw := []byte(`{"type":"sync_metadata","data":{"scope":"group","id":"123@g.us","notify":{"bot":"master","chat":123,"thread":0}}}`)
	if _, err := r.f.store.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: "manual-notifier", Kind: "management", Stream: "manual-notifier", Payload: raw}); err != nil {
		t.Fatal(err)
	}
	var job int64
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup='manual-notifier'").Scan(&job); err != nil {
		t.Fatal(err)
	}
	if result := r.f.run(t, job, "management", r.coordinator.Admit); result.State != delivery.Done {
		t.Fatal(result)
	}
	r.drain(t, 1)
	r.f.mu.Lock()
	defer r.f.mu.Unlock()
	seen := map[int64]bool{}
	for _, call := range r.f.tgCalls {
		seen[call.Chat] = true
	}
	if !seen[-100] || !seen[123] || len(seen) != 2 {
		t.Fatal("automatic source lost its default or waiter", seen)
	}
}

func TestMetadataSourcesAutomaticLabelsReachCompletedReports(t *testing.T) {
	for _, trigger := range []string{"new", "link", "bind"} {
		t.Run(trigger, func(t *testing.T) {
			r := newRunFixture(t)
			r.url = nil
			scope, id := "group", "123@g.us"
			if trigger == "bind" {
				scope, id = "contact", "12345000000001@lid"
				r.bindBot(t, "200", id, "Alice")
			} else {
				r.f.route(t, id, -200, 0)
			}
			if result := automaticMetadata(t, r, "automatic-"+trigger, "sync-request", trigger, scope, id); result.State != delivery.Done {
				t.Fatal(result)
			}
			r.drain(t, 1)
			var raw []byte
			if err := r.f.store.pool.QueryRow(t.Context(), "SELECT payload FROM metadata_reports").Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var report metadata.Report
			if json.Unmarshal(raw, &report) != nil || report.Source != trigger {
				t.Fatal(report)
			}
		})
	}
}

func TestMetadataSourcesPublicManagementCannotBecomeQuietCron(t *testing.T) {
	r := newRunFixture(t)
	r.f.route(t, "123@g.us", -200, 0)
	if result := automaticMetadata(t, r, "manual-source-override", "management", "cron", "group", "123@g.us"); result.State != delivery.Done {
		t.Fatal(result)
	}
	var source string
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT source FROM metadata_runs").Scan(&source); err != nil || source != "command" {
		t.Fatal(source, err)
	}
}

func TestMetadataSourcesInternalAdmissionsRejectInvalidTriggerScopes(t *testing.T) {
	for _, tc := range []struct{ source, scope, id string }{{"cron", "group", "123@g.us"}, {"command", "group", "123@g.us"}, {"link", "contact", "123@c.us"}, {"bind", "group", "123@g.us"}} {
		t.Run(tc.source+"-"+tc.scope, func(t *testing.T) {
			r := newRunFixture(t)
			if result := automaticMetadata(t, r, "invalid-trigger", "sync-request", tc.source, tc.scope, tc.id); result.State != delivery.Failed {
				t.Fatal(result)
			}
			var runs int
			if err := r.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM metadata_runs").Scan(&runs); err != nil || runs != 0 {
				t.Fatal(runs, err)
			}
		})
	}
}

func TestMetadataSourcesCoalescingKeepsFirstAutomaticSource(t *testing.T) {
	r := newRunFixture(t)
	r.f.route(t, "123@g.us", -200, 0)
	if result := automaticMetadata(t, r, "automatic-first", "sync-request", "link", "group", "123@g.us"); result.State != delivery.Done {
		t.Fatal(result)
	}
	if result := automaticMetadata(t, r, "manual-second", "management", "", "group", "123@g.us"); result.State != delivery.Done {
		t.Fatal(result)
	}
	var runs int
	var source string
	if err := r.f.store.pool.QueryRow(t.Context(), "SELECT count(*),max(source) FROM metadata_runs").Scan(&runs, &source); err != nil || runs != 1 || source != "link" {
		t.Fatal(runs, source, err)
	}
}
