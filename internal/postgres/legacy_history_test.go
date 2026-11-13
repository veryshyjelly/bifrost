package postgres

import (
	"fmt"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func TestLegacyHistorySkipRequiresCompletedSurvivingDestinationEvidence(t *testing.T) {
	s := testStore(t)
	source := `INSERT INTO kv VALUES('route','12345@g.us','{"chatId":"12345@g.us","name":"Group","tgChat":"-100","thread":44,"mode":"inbox"}');`
	for i, id := range []string{"done", "partial", "scoped", "unknown", "deleted", "retired"} {
		thread, deleted := 44, 0
		if id == "retired" {
			thread = 45
		}
		if id == "deleted" {
			deleted = 1
		}
		source += fmt.Sprintf("INSERT INTO messages VALUES('12345@g.us','%s','-100',%d,'master','in',0);INSERT INTO telegram_messages VALUES(%d,'*','-100',%d,'master',%d,'chat',1700000000000,0,%d,NULL);", id, 10+i, 1+i, 10+i, thread, deleted)
		if id != "unknown" && id != "scoped" {
			source += legacyValueSQL("wa-complete", "12345@g.us:"+id, "true")
		}
	}
	for i, id := range []string{"private-partial", "private-done"} {
		source += fmt.Sprintf("INSERT INTO messages VALUES('12025550123@c.us','%s','123',%d,'77','out',0);", id, 20+i)
	}
	destination := func(id string) string {
		return legacy.HistoryDestinationKey("fixture-account", "12345@g.us", id, "master", -100, 44)
	}
	source += legacyValueSQL("destination-start", destination("partial"), `{"key":"original-partial"}`)
	source += legacyValueSQL("destination-start", destination("scoped"), `{"key":"original-scoped"}`) + legacyValueSQL("destination-complete", destination("scoped"), "true")
	source += legacyValueSQL("destination-start", legacy.HistoryDestinationKey("fixture-account", "12025550123@c.us", "private-partial", "77", 123, 0), `{"key":"original-private"}`)
	source += legacyRetiredSQL("12345@g.us", 45)
	b, _ := legacyPreparationBundle(t, source)
	defer b.Close()
	if _, err := s.StageLegacyImport(t.Context(), b, legacy.SQLiteOptions{}, "planned-session"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id, chat, bot string
		tg, thread    int64
		want          bool
	}{
		{"done", "12345@g.us", "master", -100, 44, true},
		{"partial", "12345@g.us", "master", -100, 44, false},
		{"scoped", "12345@g.us", "master", -100, 44, true},
		{"unknown", "12345@g.us", "master", -100, 44, false},
		{"deleted", "12345@g.us", "master", -100, 44, false},
		{"retired", "12345@g.us", "master", -100, 45, false},
		{"done", "12345@g.us", "master", -100, 99, false},
		{"private-partial", "12025550123@c.us", "77", 123, 0, false},
		{"private-done", "12025550123@c.us", "77", 123, 0, true},
		{"private-done", "12025550123@c.us", "master", 123, 0, false},
	} {
		got, err := s.queries.HistoryDestinationDelivered(t.Context(), db.HistoryDestinationDeliveredParams{AccountID: b.Account, WhatsappChat: tc.chat, MessageID: tc.id, ChatID: tc.tg, ThreadID: tc.thread, Scope: relayScope(tc.bot, tc.tg), LegacyDestinationKey: legacy.HistoryDestinationKey(b.Account, tc.chat, tc.id, tc.bot, tc.tg, tc.thread)})
		if err != nil || got != tc.want {
			t.Fatal("legacy history destination evidence changed", tc.id, tc.thread, got, err)
		}
	}
	var jobs, effects int
	if err := s.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM jobs),(SELECT count(*) FROM effects)").Scan(&jobs, &effects); err != nil || jobs != 0 || effects != 0 {
		t.Fatal("history evidence invented runnable work or confirmations", err)
	}
}
