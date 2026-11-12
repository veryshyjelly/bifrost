package postgres

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/changes"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func legacyChangeSQL() string {
	return legacyMessageSQL() +
		legacyValueSQL("latest-edit", "12345@g.us:group-source", `{"body":"Saved edited caption","mentionedIds":["999999@lid"]}`) +
		legacyValueSQL("latest-edit", "999999@lid:private-edit", `{"body":"","mentionedIds":[]}`) +
		legacyValueSQL("outbound-edit", "12345@g.us:group-source", `"`+changeBodyHash("Own edited caption")+`"`) +
		legacyValueSQL("revoked", "12345@g.us:gone", `true`) +
		legacyValueSQL("revoked", "999999@lid:private-gone", `true`) +
		legacyValueSQL("revoked", "12345@g.us:not-confirmed", `false`)
}

func prepareChangeFixture(t *testing.T, s *Store, source string) *legacy.Bundle {
	t.Helper()
	b := prepareMessageFixture(t, s, source)
	if _, err := s.ProjectLegacyMessages(t.Context(), b.Account, [32]byte{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacySources(t.Context(), b.Account); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLegacyChangesFeedSourceNormalizationAndPreserveNativeUpdatePrecedence(t *testing.T) {
	s := testStore(t)
	b := prepareChangeFixture(t, s, legacyChangeSQL())
	result, err := s.ProjectLegacyChanges(t.Context(), b.Account)
	if err != nil || result.Edits != 2 || result.Outbound != 1 || result.Revoked != 2 {
		t.Fatal("saved changes projection incomplete", err)
	}
	latest := db.LatestMessageEditParams{AccountID: b.Account, WhatsappChat: "12345@g.us", MessageID: "group-source"}
	retained, err := s.queries.LatestMessageEdit(t.Context(), latest)
	if err != nil || retained.Body != "Saved edited caption" || retained.JobID != 0 || string(retained.MentionedIds) != `["999999@lid"]` {
		t.Fatal("retained source edit became a synthetic job or lost content", err)
	}
	for _, tc := range []struct {
		account, chat, id string
		revoked           bool
	}{
		{b.Account, "12345@g.us", "gone", true}, {b.Account, "12025550123@c.us", "private-gone", true},
		{b.Account, "12345@g.us", "not-confirmed", false}, {"another-account", "12345@g.us", "gone", false},
	} {
		got, err := s.MessageRevoked(t.Context(), tc.account, tc.chat, tc.id)
		if err != nil || got != tc.revoked {
			t.Fatal("tombstone escaped original confirmation/account scope", err)
		}
	}
	tx, err := s.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	source, err := prepareSource(t.Context(), s.queries.WithTx(tx), b.Account, sdk.ChatHistoryMessage{ID: "group-source", ChatID: "12345@g.us", Author: "888888@lid", Body: "Stale original text"})
	rollback(tx)
	if err != nil || source.Body != "Saved edited caption" || !reflect.DeepEqual(source.MentionedIDs, []string{"999999@lid"}) {
		t.Fatal("history/source normalization ignored retained latest edit", err)
	}
	if ready := s.LegacyRuntimeReady(t.Context(), b.Account); !errors.Is(ready, ErrLegacyPending) {
		t.Fatal("change projection activated incomplete account")
	}
	var jobs, effects int
	if err := s.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM jobs),(SELECT count(*) FROM effects)").Scan(&jobs, &effects); err != nil || jobs != 0 || effects != 0 {
		t.Fatal("retained changes fabricated operation evidence", err)
	}
	if _, err := s.pool.Exec(t.Context(), "UPDATE legacy_imports SET activated_at=now()"); err != nil {
		t.Fatal(err)
	}
	c := changes.Settings{Session: "fixture-session", OwnerID: 123, Management: -100, NoticeTTL: 60}
	job := legacyConsumerJob(t, s, "retained-edit-echo", "message.edited", map[string]any{"chatId": "12345@g.us", "messageId": "group-source", "body": "Own edited caption"})
	in, err := s.EditInput(t.Context(), job, c)
	if err != nil || in.Event != "" {
		t.Fatal("confirmed original outbound edit echoed", err)
	}
	finish(t, s, job)
	current, err := s.queries.LatestMessageEdit(t.Context(), latest)
	if err != nil || current.Body != "Own edited caption" || current.JobID != job.ID || string(current.MentionedIds) != `["999999@lid"]` {
		t.Fatal("native body failed to supersede retained edit or lost unspecified mentions", err)
	}
	job = legacyConsumerJob(t, s, "post-import-edit", "message.edited", map[string]any{"chatId": "12345@g.us", "messageId": "group-source", "body": "New caption", "mentionedIds": []string{}})
	in, err = s.EditInput(t.Context(), job, c)
	if err != nil || in.Event != "whatsapp.edit" || len(in.Destinations) != 2 || in.MentionedIDs == nil || len(in.MentionedIDs) != 0 {
		t.Fatal("new native edit did not clear copied mentions", err)
	}
	finish(t, s, job)
	current, err = s.queries.LatestMessageEdit(t.Context(), latest)
	if err != nil || current.Body != "New caption" || string(current.MentionedIds) != `[]` {
		t.Fatal("explicit native empty mentions fell back to imported mentions", err)
	}
	job = legacyConsumerJob(t, s, "edit-after-revocation", "message.edited", map[string]any{"chatId": "12345@g.us", "messageId": "gone", "body": "Must stay revoked"})
	in, err = s.EditInput(t.Context(), job, c)
	if err != nil || in.Event != "" {
		t.Fatal("revoked source admitted a new edit", err)
	}
	finish(t, s, job)
	if _, err = s.queries.LatestMessageEdit(t.Context(), db.LatestMessageEditParams{AccountID: b.Account, WhatsappChat: "12345@g.us", MessageID: "gone"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("revoked edit changed latest content", err)
	}
	replay, err := s.ProjectLegacyChanges(t.Context(), b.Account)
	if err != nil || !reflect.DeepEqual(result, replay) {
		t.Fatal("projection replay reset native work", err)
	}
	current, err = s.queries.LatestMessageEdit(t.Context(), latest)
	if err != nil || current.Body != "New caption" {
		t.Fatal("projection replay overwrote new native edit", err)
	}
	var raw string
	if err = s.pool.QueryRow(t.Context(), "SELECT source_value FROM legacy_values WHERE kind='latest-edit' AND source_id='12345@g.us:group-source'").Scan(&raw); err != nil || raw != `{"body":"Saved edited caption","mentionedIds":["999999@lid"]}` {
		t.Fatal("source evidence mutated", err)
	}
}

func TestLegacyChangeProjectionRollsBackAllStateAndRefusesNativeConflict(t *testing.T) {
	s := testStore(t)
	b := prepareChangeFixture(t, s, legacyChangeSQL())
	_, err := s.pool.Exec(t.Context(), `CREATE FUNCTION reject_change_projection() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END; $$;
CREATE TRIGGER reject_change_projection BEFORE INSERT ON legacy_change_projections FOR EACH ROW EXECUTE FUNCTION reject_change_projection();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectLegacyChanges(t.Context(), b.Account); err == nil {
		t.Fatal("failed checkpoint committed")
	}
	for _, table := range []string{"retained_message_edits", "message_outbound_edits", "message_tombstones", "legacy_change_projections"} {
		var n int
		if err = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
			t.Fatal("partial saved change survived rollback", table, err)
		}
	}
	if _, err = s.pool.Exec(t.Context(), "DROP TRIGGER reject_change_projection ON legacy_change_projections"); err != nil {
		t.Fatal(err)
	}
	if err = s.queries.SetMessageRevoked(t.Context(), db.SetMessageRevokedParams{AccountID: b.Account, WhatsappChat: "12345@g.us", MessageID: "existing-native"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectLegacyChanges(t.Context(), b.Account); !errors.Is(err, ErrLegacyDestination) {
		t.Fatal("native state overwritten", err)
	}
	if _, err = s.ProjectLegacyChanges(t.Context(), "another-account"); !errors.Is(err, ErrLegacyDestination) {
		t.Fatal("another account accepted", err)
	}
}

func TestLegacyChangesRequireSourceStageAndKeepEmptyCaptionDistinct(t *testing.T) {
	s := testStore(t)
	b := prepareMessageFixture(t, s, legacyChangeSQL())
	if _, err := s.ProjectLegacyChanges(t.Context(), b.Account); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("missing prerequisite accepted", err)
	}
	if _, err := s.ProjectLegacyMessages(t.Context(), b.Account, [32]byte{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacySources(t.Context(), b.Account); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacyChanges(t.Context(), b.Account); err != nil {
		t.Fatal(err)
	}
	e, err := s.queries.LatestMessageEdit(t.Context(), db.LatestMessageEditParams{AccountID: b.Account, WhatsappChat: "12025550123@c.us", MessageID: "private-edit"})
	var ids []string
	if err != nil || e.Body != "" || json.Unmarshal(e.MentionedIds, &ids) != nil || ids == nil || len(ids) != 0 {
		t.Fatal("saved empty caption or empty mention list lost", err)
	}
}
