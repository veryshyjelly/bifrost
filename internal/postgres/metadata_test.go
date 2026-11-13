package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/metadata"
	wa "github.com/veryshyjelly/bifrost/internal/providers/openwa"
	tg "github.com/veryshyjelly/bifrost/internal/providers/telegram"
)

type metadataCall struct {
	Method, Bot, Text string
	Chat, Thread      string
}
type metadataFixture struct {
	f                        *relayFixture
	service                  *metadata.Service
	wa                       *wa.Client
	clients                  metadata.Clients
	mu                       sync.Mutex
	group, contact           string
	phoneProof               string
	groupByID, contactByID   map[string]string
	refuseMethod             string
	groupReads, contactReads int
	public, canChange        bool
	failAt, dropAt           int
	title, description       string
	calls                    []metadataCall
	beforeWrite              func()
}

func newMetadataFixture(t *testing.T, withMentions ...bool) *metadataFixture {
	t.Helper()
	m := &metadataFixture{f: newRelayFixture(t, nil, withMentions...), canChange: true, title: "Old", description: "Old description", group: `{"id":"123@g.us","name":"New group","participants":[{"id":"12345000000001@lid","number":"2025550123","name":"Friend"}],"description":"New description"}`, contact: `{"id":"12345000000001@lid","name":"Friend","number":"12345000000001"}`}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			m.mu.Lock()
			defer m.mu.Unlock()
			if r.Method != "GET" {
				t.Error("metadata mutated WhatsApp", r.Method, r.URL.Path)
			}
			switch {
			case strings.Contains(r.URL.Path, "/groups/"):
				m.groupReads++
				body := m.group
				if value := m.groupByID[r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]]; value != "" {
					body = value
				}
				fmt.Fprint(w, body)
			case strings.HasSuffix(r.URL.Path, "/phone"):
				fmt.Fprint(w, m.phoneProof)
			case strings.Contains(r.URL.Path, "/contacts/"):
				m.contactReads++
				body := m.contact
				if value := m.contactByID[r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]]; value != "" {
					body = value
				}
				fmt.Fprint(w, body)
			default:
				t.Error("unexpected source API", r.URL.Path)
				w.WriteHeader(500)
			}
			return
		}
		if strings.Contains(r.Header.Get("Content-Type"), "multipart/") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
		} else if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		botID := strings.Split(strings.TrimPrefix(r.URL.Path, "/bot"), "/")[0]
		switch method {
		case "getMe":
			id := "999"
			if botID != "master" {
				id = botID
			}
			fmt.Fprintf(w, `{"ok":true,"result":{"id":%s,"is_bot":true,"first_name":"Old bot","username":"fixture_bot"}}`, id)
			return
		case "getChat":
			m.mu.Lock()
			defer m.mu.Unlock()
			username := ""
			if m.public {
				username = "public"
			}
			fmt.Fprintf(w, `{"ok":true,"result":{"id":%s,"type":"supergroup","title":%q,"description":%q,"username":%q}}`, r.FormValue("chat_id"), m.title, m.description, username)
			return
		case "getChatMember":
			m.mu.Lock()
			defer m.mu.Unlock()
			fmt.Fprintf(w, `{"ok":true,"result":{"status":"administrator","can_change_info":%t,"user":{"id":999,"is_bot":true,"first_name":"Fixture"}}}`, m.canChange)
			return
		case "getMyDescription":
			fmt.Fprint(w, `{"ok":true,"result":{"description":"Old bot description"}}`)
			return
		case "editForumTopic", "setChatTitle", "setChatDescription", "setMyName", "setMyDescription":
		default:
			t.Error("unexpected Telegram operation", method)
			w.WriteHeader(500)
			return
		}
		text := r.FormValue("name")
		if method == "setChatTitle" {
			text = r.FormValue("title")
		}
		if method == "setChatDescription" || method == "setMyDescription" {
			text = r.FormValue("description")
		}
		m.mu.Lock()
		m.calls = append(m.calls, metadataCall{Method: method, Bot: botID, Text: text, Chat: r.FormValue("chat_id"), Thread: r.FormValue("message_thread_id")})
		n, fail, drop, before := len(m.calls), m.failAt, m.dropAt, m.beforeWrite
		m.mu.Unlock()
		if before != nil {
			before()
		}
		if n == drop {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
			} else {
				conn.Close()
			}
			return
		}
		if n == fail {
			fmt.Fprint(w, `{"ok":false,"error_code":429,"parameters":{"retry_after":60}}`)
			return
		}
		if method == m.refuseMethod {
			fmt.Fprint(w, `{"ok":false,"error_code":403}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	}))
	t.Cleanup(server.Close)
	var err error
	m.wa, err = wa.New(server.URL, "fixture-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	clients := map[string]*tg.Client{}
	for _, id := range []string{"master", "200", "201"} {
		clients[id], err = tg.New(server.URL, id, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	m.clients = func(_ context.Context, account, id string) (metadata.Telegram, error) {
		if account != "fixture-account" || clients[id] == nil {
			return nil, delivery.ErrInvalid
		}
		return clients[id], nil
	}
	m.service, err = metadata.New(m.f.store, m.wa, m.clients, "fixture-session", -100)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (m *metadataFixture) enqueue(t *testing.T, key, scope, id string, run ...string) int64 {
	t.Helper()
	data := map[string]string{"scope": scope, "id": id, "source": "fixture", "requestId": key}
	if len(run) > 0 {
		data["run"] = run[0]
		data["session"] = "fixture-session"
	}
	payload, _ := json.Marshal(map[string]any{"type": "sync_metadata", "data": data})
	if _, err := m.f.store.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: key, Stream: key, Kind: "management", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var jobID int64
	if err := m.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", key).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	return jobID
}
func (m *metadataFixture) run(t *testing.T, id int64) delivery.Report {
	t.Helper()
	return m.f.run(t, id, "management", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		if _, err := m.service.Prepare(ctx, j); err != nil {
			return err
		}
		return m.service.ApplyText(ctx, j, e)
	})
}
func (m *metadataFixture) retry(t *testing.T, id int64) {
	t.Helper()
	if _, err := m.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1 AND state='pending'", id); err != nil {
		t.Fatal(err)
	}
}

func TestMetadataInboxRefreshRenamesOnlyTopicAndPersistsCompleteRoster(t *testing.T) {
	m := newMetadataFixture(t)
	m.f.route(t, "123@g.us", -100, 44)
	id := m.enqueue(t, "metadata-inbox", "group", "123@g.us")
	if r := m.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(m.calls) != 1 || m.calls[0].Method != "editForumTopic" || m.calls[0].Chat != "-100" || m.calls[0].Thread != "44" || m.calls[0].Text != "New group" {
		t.Fatal(m.calls)
	}
	roster, found, err := m.f.store.MentionRoster(t.Context(), "fixture-account", "123@g.us")
	if err != nil || !found || roster.Name != "New group" || len(roster.Members) != 1 || roster.Members[0].ID != "12345000000001@lid" {
		t.Fatal(roster, err)
	}
	var aliases int
	if err := m.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM contact_aliases").Scan(&aliases); err != nil || aliases != 0 {
		t.Fatal("display number became alias", aliases, err)
	}
	var complete bool
	if err := m.f.store.pool.QueryRow(t.Context(), "SELECT (payload->>'TextComplete')::boolean FROM metadata_text_plans WHERE job_id=$1", id).Scan(&complete); err != nil || !complete {
		t.Fatal(complete, err)
	}
	if m.groupReads != 1 {
		t.Fatal(m.groupReads)
	}
}

func TestMetadataMirrorPreservesUnavailableDescriptionsAndAllowsExplicitEmpty(t *testing.T) {
	for _, tc := range []struct {
		name, extra string
		want        int
	}{
		{"missing", "", 1}, {"null", `,"description":null`, 1}, {"empty", `,"description":""`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMetadataFixture(t)
			m.f.route(t, "123@g.us", -200, 0)
			m.group = `{"id":"123@g.us","name":"New group","participants":[]` + tc.extra + `}`
			id := m.enqueue(t, "metadata-mirror", "group", "123@g.us")
			if r := m.run(t, id); r.State != delivery.Done {
				t.Fatal(r)
			}
			if len(m.calls) != tc.want || m.calls[0].Method != "setChatTitle" || m.calls[0].Chat != "-200" {
				t.Fatal(m.calls)
			}
			if tc.want == 2 && (m.calls[1].Method != "setChatDescription" || m.calls[1].Text != "") {
				t.Fatal(m.calls)
			}
		})
	}
}

func TestMetadataInvalidGroupPreservesPreviousRosterAndIdentity(t *testing.T) {
	m := newMetadataFixture(t)
	m.f.route(t, "123@g.us", -100, 44)
	id := m.enqueue(t, "metadata-good", "group", "123@g.us")
	if r := m.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	m.group = `{"id":"123@g.us","name":"Broken","participants":null}`
	id = m.enqueue(t, "metadata-broken", "group", "123@g.us")
	if r := m.run(t, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	roster, _, err := m.f.store.MentionRoster(t.Context(), "fixture-account", "123@g.us")
	if err != nil || roster.Name != "New group" || len(roster.Members) != 1 || len(m.calls) != 1 {
		t.Fatal(roster, m.calls, err)
	}
	var name string
	if err := m.f.store.pool.QueryRow(t.Context(), "SELECT name FROM bridge_routes").Scan(&name); err != nil || name != "New group" {
		t.Fatal(name, err)
	}
}

func TestMetadataTextRetryFreezesValuesAndResumesAfterConfirmedPart(t *testing.T) {
	m := newMetadataFixture(t)
	m.f.route(t, "123@g.us", -200, 0)
	m.failAt = 2
	id := m.enqueue(t, "metadata-retry", "group", "123@g.us")
	if r := m.run(t, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	m.group = `{"id":"123@g.us","name":"Later","participants":[],"description":"Later"}`
	reopened, err := Open(t.Context(), m.f.store.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	m.service, err = metadata.New(reopened, m.wa, m.clients, "fixture-session", -100)
	if err != nil {
		t.Fatal(err)
	}
	m.retry(t, id)
	if r := m.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(m.calls) != 3 || m.calls[0].Method != "setChatTitle" || m.calls[1].Method != "setChatDescription" || m.calls[2].Method != "setChatDescription" || m.calls[2].Text != "New description" || m.groupReads != 1 {
		t.Fatal(m.calls, m.groupReads)
	}
}

func TestMetadataLostSetterResponseRetriesIdempotently(t *testing.T) {
	m := newMetadataFixture(t)
	m.f.route(t, "123@g.us", -100, 44)
	m.dropAt = 1
	id := m.enqueue(t, "metadata-drop", "group", "123@g.us")
	if r := m.run(t, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	m.retry(t, id)
	if r := m.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(m.calls) != 2 || m.calls[0] != m.calls[1] || m.groupReads != 1 {
		t.Fatal(m.calls, m.groupReads)
	}
}

func TestMetadataRouteOrPublicUsernameChangeStopsPreparedText(t *testing.T) {
	for _, change := range []string{"route", "generation", "public"} {
		t.Run(change, func(t *testing.T) {
			m := newMetadataFixture(t)
			m.f.route(t, "123@g.us", -200, 0)
			m.failAt = 2
			id := m.enqueue(t, "metadata-change", "group", "123@g.us")
			if r := m.run(t, id); r.State != delivery.Pending {
				t.Fatal(r)
			}
			switch change {
			case "route":
				_, err := m.f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET telegram_chat_id=-201")
				if err != nil {
					t.Fatal(err)
				}
			case "generation":
				_, err := m.f.store.pool.Exec(t.Context(), "INSERT INTO mirror_returns(account_id,whatsapp_chat,return_key) VALUES('fixture-account','123@g.us','later')")
				if err != nil {
					t.Fatal(err)
				}
			case "public":
				m.public = true
			}
			m.retry(t, id)
			if r := m.run(t, id); r.State != delivery.Failed {
				t.Fatal(r)
			}
			if len(m.calls) != 2 {
				t.Fatal("obsolete setter repeated", m.calls)
			}
		})
	}
}

func TestMetadataMissingPermissionRetainsSnapshotAndWarningWithoutTextWrites(t *testing.T) {
	m := newMetadataFixture(t)
	m.f.route(t, "123@g.us", -200, 0)
	m.canChange = false
	id := m.enqueue(t, "metadata-permission", "group", "123@g.us")
	if r := m.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	var warning string
	if err := m.f.store.pool.QueryRow(t.Context(), "SELECT payload->>'Warning' FROM metadata_text_plans WHERE job_id=$1", id).Scan(&warning); err != nil || warning == "" || len(m.calls) != 0 {
		t.Fatal(warning, m.calls, err)
	}
	if _, found, err := m.f.store.MentionRoster(t.Context(), "fixture-account", "123@g.us"); err != nil || !found {
		t.Fatal(found, err)
	}
}

func TestMetadataBoundContactWithoutDirectRouteUpdatesOnlyItsBot(t *testing.T) {
	m := newMetadataFixture(t)
	m.f.bind(t, "200", "12345000000001@lid")
	id := m.enqueue(t, "metadata-contact", "contact", "12345000000001@lid")
	if r := m.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(m.calls) != 2 || m.calls[0].Method != "setMyName" || m.calls[0].Bot != "200" || m.calls[0].Text != "Friend" || m.calls[1].Method != "setMyDescription" || m.contactReads != 1 {
		t.Fatal(m.calls)
	}
	d, err := m.f.store.IdentityDirectory(t.Context(), "fixture-account")
	if err != nil {
		t.Fatal(err)
	}
	person := d.Person("12345000000001@lid", nil)
	if person.ID != "12345000000001@lid" || person.Name != "Friend" || person.Bot.ID != "200" {
		t.Fatal(person)
	}
	var routes int
	if err := m.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM bridge_routes").Scan(&routes); err != nil || routes != 0 {
		t.Fatal("refresh created route", routes, err)
	}
}

func TestMetadataBindingChangeBlocksPreparedBotUpdate(t *testing.T) {
	m := newMetadataFixture(t)
	m.f.bind(t, "200", "12345000000001@lid")
	m.failAt = 2
	id := m.enqueue(t, "metadata-binding", "contact", "12345000000001@lid")
	if r := m.run(t, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	m.f.bind(t, "201", "12345000000001@lid")
	m.retry(t, id)
	if r := m.run(t, id); r.State != delivery.Failed {
		t.Fatal(r)
	}
	if len(m.calls) != 2 {
		t.Fatal("obsolete bot update repeated", m.calls)
	}
}

func TestMetadataPlanFailureRollsBackRosterRouteAndSnapshot(t *testing.T) {
	m := newMetadataFixture(t)
	m.f.route(t, "123@g.us", -100, 44)
	if _, err := m.f.store.pool.Exec(t.Context(), `CREATE FUNCTION reject_metadata_plan() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture rollback'; END $$; CREATE TRIGGER reject_metadata BEFORE INSERT ON metadata_text_plans FOR EACH ROW EXECUTE FUNCTION reject_metadata_plan()`); err != nil {
		t.Fatal(err)
	}
	id := m.enqueue(t, "metadata-rollback", "group", "123@g.us")
	if r := m.run(t, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	var count int
	for _, table := range []string{"metadata_snapshots", "metadata_text_plans", "mention_rosters", "metadata_topic_names"} {
		if err := m.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
	var name string
	if err := m.f.store.pool.QueryRow(t.Context(), "SELECT name FROM bridge_routes").Scan(&name); err != nil || name == "New group" || len(m.calls) != 0 {
		t.Fatal(name, m.calls, err)
	}
}

func TestMetadataFailedTopicDoesNotForgetLastAppliedName(t *testing.T) {
	m := newMetadataFixture(t)
	m.f.route(t, "123@g.us", -100, 44)
	m.failAt = 1
	id := m.enqueue(t, "metadata-topic-failed", "group", "123@g.us")
	if r := m.run(t, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	// A separate request still sees the unapplied original topic name even
	// though the stored source name has already been refreshed.
	later := m.enqueue(t, "metadata-topic-new", "group", "123@g.us")
	if r := m.run(t, later); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(m.calls) != 2 || m.calls[1].Method != "editForumTopic" {
		t.Fatal(m.calls)
	}
	m.retry(t, id)
	if r := m.run(t, id); r.State != delivery.Failed {
		t.Fatal(r)
	}
	if len(m.calls) != 2 {
		t.Fatal("older snapshot overwrote latest", m.calls)
	}
}

func TestMetadataClaimsAndAccountsFenceAllSnapshotWrites(t *testing.T) {
	m := newMetadataFixture(t)
	m.f.route(t, "123@g.us", -100, 44)
	m.enqueue(t, "metadata-fence", "group", "123@g.us")
	j := claim(t, m.f.store)
	target, err := m.f.store.MetadataTarget(t.Context(), j, "fixture-session", -100)
	if err != nil {
		t.Fatal(err)
	}
	forged := j
	forged.AccountID = "other-account"
	if _, err := m.f.store.MetadataTarget(t.Context(), forged, "fixture-session", -100); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
	if _, err := m.f.store.pool.Exec(t.Context(), "UPDATE jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", j.ID); err != nil {
		t.Fatal(err)
	}
	_, err = m.f.store.PrepareMetadata(t.Context(), j, target, metadata.Snapshot{ID: "123@g.us", Scope: "group", Name: "New", FetchedAt: time.Now().UnixMilli(), Members: []identity.Member{}}, nil, "")
	if !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
}

type metadataTextRecordFailure struct {
	*Store
	failed bool
}

func (s *metadataTextRecordFailure) CompleteMetadataText(ctx context.Context, j delivery.Job) error {
	if !s.failed {
		s.failed = true
		return errors.New("fixture text completion failure")
	}
	return s.Store.CompleteMetadataText(ctx, j)
}

func TestMetadataConfirmedTextReceiptReconcilesWithoutAnotherSetter(t *testing.T) {
	m := newMetadataFixture(t)
	m.f.route(t, "123@g.us", -100, 44)
	wrapped := &metadataTextRecordFailure{Store: m.f.store}
	var err error
	m.service, err = metadata.New(wrapped, m.wa, m.clients, "fixture-session", -100)
	if err != nil {
		t.Fatal(err)
	}
	id := m.enqueue(t, "metadata-reconcile", "group", "123@g.us")
	if r := m.run(t, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	m.retry(t, id)
	if r := m.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(m.calls) != 1 || m.groupReads != 1 {
		t.Fatal("confirmed setter repeated", m.calls, m.groupReads)
	}
}

func TestMetadataRetiredInboxCannotBeRenamed(t *testing.T) {
	m := newMetadataFixture(t)
	m.f.route(t, "123@g.us", -100, 44)
	if _, err := m.f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET inbox_retired=true"); err != nil {
		t.Fatal(err)
	}
	id := m.enqueue(t, "metadata-retired", "group", "123@g.us")
	if r := m.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(m.calls) != 0 {
		t.Fatal("retired topic touched", m.calls)
	}
}

func TestMetadataVerifiedAliasKeepsContactBindingAndSharesSnapshotScope(t *testing.T) {
	m := newMetadataFixture(t)
	m.f.bind(t, "200", "12345000000001@lid")
	if _, err := m.f.store.pool.Exec(t.Context(), "INSERT INTO contact_aliases(account_id,alias,canonical) VALUES('fixture-account','12345000000001@lid','12025550101@c.us')"); err != nil {
		t.Fatal(err)
	}
	id := m.enqueue(t, "metadata-alias", "contact", "12345000000001@lid")
	if r := m.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	d, err := m.f.store.IdentityDirectory(t.Context(), "fixture-account")
	if err != nil {
		t.Fatal(err)
	}
	p := d.Person("12025550101@c.us", nil)
	if p.Bot.ID != "200" || p.Name != "Friend" {
		t.Fatal(p)
	}
	var source string
	if err := m.f.store.pool.QueryRow(t.Context(), "SELECT source_id FROM metadata_snapshots").Scan(&source); err != nil || source != "12025550101@c.us" {
		t.Fatal(source, err)
	}
}

func TestMetadataContactUsesPushNameThenStoredNameWithoutCreatingBots(t *testing.T) {
	for _, tc := range []struct{ name, payload, want string }{
		{"push name", `{"id":"12345000000001@lid","name":" ","pushName":" Friend "}`, "Friend"},
		{"stored name", `{"id":"12345000000001@lid","name":null,"pushName":" "}`, "Stored contact"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMetadataFixture(t)
			m.f.route(t, "12345000000001@lid", -100, 44)
			m.contact = tc.payload
			if _, err := m.f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET name='Stored contact'"); err != nil {
				t.Fatal(err)
			}
			id := m.enqueue(t, "metadata-contact-fallback", "contact", "12345000000001@lid")
			if r := m.run(t, id); r.State != delivery.Done {
				t.Fatal(r)
			}
			for _, c := range m.calls {
				if c.Method != "editForumTopic" || c.Text != tc.want {
					t.Fatal(m.calls)
				}
			}
			var bots int
			if err := m.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM bridge_contact_bots").Scan(&bots); err != nil || bots != 0 {
				t.Fatal(bots, err)
			}
			var name string
			if err := m.f.store.pool.QueryRow(t.Context(), "SELECT name FROM contact_profiles").Scan(&name); err != nil || name != tc.want {
				t.Fatal(name, err)
			}
		})
	}
}

func TestMetadataRouteChangedDuringProviderReadCannotCommitSnapshot(t *testing.T) {
	m := newMetadataFixture(t)
	m.f.route(t, "123@g.us", -100, 44)
	m.enqueue(t, "metadata-read-race", "group", "123@g.us")
	j := claim(t, m.f.store)
	target, err := m.f.store.MetadataTarget(t.Context(), j, "fixture-session", -100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET telegram_chat_id=-200,thread_id=0"); err != nil {
		t.Fatal(err)
	}
	_, err = m.f.store.PrepareMetadata(t.Context(), j, target, metadata.Snapshot{ID: "123@g.us", Scope: "group", Name: "New", FetchedAt: time.Now().UnixMilli(), Members: []identity.Member{}}, nil, "")
	if !errors.Is(err, metadata.ErrTarget) {
		t.Fatal(err)
	}
	var count int
	if err := m.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM metadata_snapshots").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestMetadataTextReceiptComparisonPreservesIntegerPrecision(t *testing.T) {
	m := newMetadataFixture(t)
	m.f.route(t, "123@g.us", -9007199254740993, 0)
	m.group = `{"id":"123@g.us","name":"New group","participants":[]}`
	var err error
	m.service, err = metadata.New(&metadataTextRecordFailure{Store: m.f.store}, m.wa, m.clients, "fixture-session", -100)
	if err != nil {
		t.Fatal(err)
	}
	id := m.enqueue(t, "metadata-precision", "group", "123@g.us")
	if r := m.run(t, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	// Adjacent IDs above 2^53 must not compare equal through float64 decoding.
	if _, err := m.f.store.pool.Exec(t.Context(), `UPDATE effects SET request=jsonb_set(request,'{Target,Route,ChatID}','-9007199254740992') WHERE job_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	m.retry(t, id)
	j := claim(t, m.f.store)
	if err := m.f.store.CompleteMetadataText(t.Context(), j); !errors.Is(err, delivery.ErrInvalid) {
		t.Fatal(err)
	}
}
