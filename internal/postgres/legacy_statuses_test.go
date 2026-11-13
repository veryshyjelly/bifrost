package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func statusTupleForPG(parts ...string) string {
	raw, _ := json.Marshal(parts)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func legacyStatusSQL() string {
	scope := legacy.StatusScope("fixture-account")
	id := statusTupleForPG(scope, "999999@lid")
	mute, _ := json.Marshal(map[string]any{"id": id, "session": scope, "sender": "999999@lid", "name": "Original author", "active": true, "revision": 7, "updatedAt": 1700000000000})
	return legacyIncomingReactionSQL() + legacyValueSQL("status-feed", statusTupleForPG("fixture-account", "-100"), `{"chatId":"status@broadcast","tgChat":"-100","thread":60,"mode":"status"}`) + legacyValueSQL("status-mute", id, string(mute)) + legacyValueSQL("status-mute-counter", scope, "9") + legacyValueSQL("status-skipped", legacy.StatusSkipKey("fixture-account", "never-enqueued"), `{"sender":"999999@lid","at":1700000000000}`) + legacyValueSQL("status-mute-op", statusTupleForPG(scope, "123", "tg:master:201"), string(mute)) + legacyValueSQL("wa-complete", "status@broadcast:old-post", "true") + legacyValueSQL("wamessage", "status@broadcast:old-post", `{"sender":"999999@lid","name":"Original author","status":true}`) + `INSERT INTO messages VALUES('status@broadcast','old-post','-100',50,'master','in',0);
INSERT INTO telegram_messages VALUES(9007199254740998,'*','-100',50,'master',60,'chat',1700000000123,0,0,NULL);
INSERT INTO jobs VALUES(201,'tg:master:201','tg:master:-100:60','tg','{"bot":"master","update":{"update_id":201,"message":{"message_id":201,"chat":{"id":-100},"from":{"id":123},"text":"/mute_status"}}}','done',1,0,NULL,1700000000123);`
}
func prepareStatusFixture(t *testing.T, s *Store) *legacy.Bundle {
	t.Helper()
	b := prepareIncomingReactionFixture(t, s, legacyStatusSQL())
	if _, err := s.ProjectLegacyIncomingReactions(t.Context(), b.Account); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLegacyStatusesFeedNativeOwnershipMuteOperationsAndPermanentSuppression(t *testing.T) {
	s := testStore(t)
	b := prepareStatusFixture(t, s)
	result, err := s.ProjectLegacyStatuses(t.Context(), b.Account, [32]byte{})
	if err != nil || !result.Feed || result.Revision != 9 || result.Mutes != 1 || result.Skipped != 1 || result.Operations != 1 {
		t.Fatal("Status projection incomplete", err)
	}
	feed, found, err := s.StatusTopic(t.Context(), b.Account, -100)
	if err != nil || !found || feed.ThreadID != 60 {
		t.Fatal("native topic did not retain feed", err)
	}
	other, err := s.queries.OtherTopicRoute(t.Context(), db.OtherTopicRouteParams{AccountID: b.Account, TelegramChatID: -100, ThreadID: 60, WhatsappChat: "foreign@g.us"})
	if err != nil || !other {
		t.Fatal("saved feed topic was reusable", err)
	}
	for _, id := range []string{"old-post", "never-enqueued"} {
		skip, err := legacyStatusSuppressed(t.Context(), s.queries, b.Account, id)
		if err != nil || !skip {
			t.Fatal("old completed/skipped Status could reappear", err)
		}
	}
	raw, err := s.queries.StatusReplySource(t.Context(), db.StatusReplySourceParams{AccountID: b.Account, PostID: "old-post"})
	if err != nil {
		t.Fatal(err)
	}
	var source struct{ SenderID string }
	_ = json.Unmarshal(raw, &source)
	if source.SenderID != "12025550123@c.us" {
		t.Fatal("reply lost original author")
	}
	d, err := directory(t.Context(), s.queries, b.Account)
	if err != nil {
		t.Fatal(err)
	}
	mute, err := setStatusMute(t.Context(), s.queries, db.Job{AccountID: b.Account, Dedup: "tg:master:201", ID: 999}, d, "12025550123@c.us", "new name", false)
	if err != nil || !mute.Active || mute.Revision != 7 {
		t.Fatal("original operation was reapplied", err)
	}
	next, err := s.queries.NextStatusMuteRevision(t.Context(), b.Account)
	if err != nil || next != 10 {
		t.Fatal("mute revision rewound", err)
	}
	replay, err := s.ProjectLegacyStatuses(t.Context(), b.Account, [32]byte{})
	if err != nil || !reflect.DeepEqual(result, replay) {
		t.Fatal("replay reset Status state", err)
	}
	if !errors.Is(s.LegacyRuntimeReady(context.Background(), b.Account), ErrLegacyPending) {
		t.Fatal("Status preparation activated import")
	}
}

func TestLegacyStatusProjectionRollsBackAllCapabilities(t *testing.T) {
	s := testStore(t)
	b := prepareStatusFixture(t, s)
	_, err := s.pool.Exec(t.Context(), `CREATE FUNCTION reject_status_projection() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture'; END; $$; CREATE TRIGGER reject_status_projection BEFORE INSERT ON legacy_status_projections FOR EACH ROW EXECUTE FUNCTION reject_status_projection();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectLegacyStatuses(t.Context(), b.Account, [32]byte{}); err == nil {
		t.Fatal("checkpoint failure committed")
	}
	for _, table := range []string{"retained_status_feeds", "retained_status_skips", "retained_status_mute_operations", "status_mutes", "status_mute_counters"} {
		var n int
		if err = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
			t.Fatal("partial Status state survived", table, err)
		}
	}
}
