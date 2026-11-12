package postgres

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/changes"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/reactions"
	"github.com/veryshyjelly/bifrost/internal/revocations"
)

func legacyValueSQL(kind, id, value string) string {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	return "INSERT INTO kv VALUES(" + quote(kind) + "," + quote(id) + "," + quote(value) + ");"
}
func legacyReceiptKeyForPG(bot, chat string, id int64) string {
	raw, _ := json.Marshal([]any{bot, chat, id})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func legacyMessageSQL() string {
	format := `{"bot":"master","sender":"12025550999@c.us","name":"Original linked name","own":true,"historical":true,"timestamp":1700000000,"historyStyle":"compact","timeZone":"Asia/Kolkata"}`
	return `
INSERT INTO kv VALUES('route','12345@g.us','{"chatId":"12345@g.us","name":"Group","tgChat":"-100","thread":44,"mode":"inbox"}');
INSERT INTO messages VALUES('12345@g.us','group-source','-100',10,'master','in',0);
INSERT INTO messages VALUES('12345@g.us','group-source','-100',11,'master','in',1);
INSERT INTO messages VALUES('999999@lid','private-owner','123',42,'77','out',1);
INSERT INTO messages VALUES('12025550123@c.us','private-master','123',42,'master','in',0);
INSERT INTO messages VALUES('12345@g.us','group-source','-100',12,'master','in',1);
INSERT INTO telegram_messages VALUES(9007199254740993,'*','-100',10,'master',44,'chat',1700000000123,0,0,NULL);
INSERT INTO telegram_messages VALUES(9007199254740994,'*','-100',11,'master',44,'chat',1700000000123,0,0,NULL);
INSERT INTO telegram_messages VALUES(9007199254740995,'77','123',42,'77',0,'chat',1700000000123,0,0,'original cleanup failure');
INSERT INTO telegram_messages VALUES(9007199254740996,'master','123',42,'master',0,'log',1700000000123,1700000060123,0,NULL);
INSERT INTO telegram_messages VALUES(9007199254740997,'*','-100',12,'master',99,'chat',1700000000123,0,0,'old retired error');
INSERT INTO jobs VALUES(100,'tg:77:100','tg:77:123:0','tg','{"bot":"77","update":{"update_id":100,"message":{"message_id":42,"chat":{"id":123,"type":"private"},"from":{"id":123,"is_bot":false},"photo":[{"file_id":"owner-77-file","file_unique_id":"owner-77-unique","width":512,"height":512}]}}}','done',1,0,NULL,1700000000123);
` + legacyValueSQL("telegram-receipt", legacyReceiptKeyForPG("master", "-100", 10), `{"message_id":10,"chat":{"id":-100},"from":{"id":999,"is_bot":true},"message_thread_id":44,"text":"Original label","future_field":{"retained":true}}`) +
		legacyValueSQL("telegram-receipt", legacyReceiptKeyForPG("master", "123", 42), `{"message_id":42,"chat":{"id":123},"from":{"id":999,"is_bot":true},"text":"master private copy"}`) +
		legacyValueSQL("message-presentation", legacyReceiptKeyForPG("master", "-100", 10), format) + legacyValueSQL("message-presentation", legacyReceiptKeyForPG("master", "-100", 11), format) +
		legacyValueSQL("wamessage", "12345@g.us:group-source", `{"historical":true,"fromMe":true,"sender":"12025550999@c.us","name":"Source name","mentionedIds":["999999@lid"]}`) +
		legacyValueSQL("reaction-self", legacyDirectoryScopeForPG("fixture-account"), `{"sender":"12025550999@c.us"}`) + legacyRetiredSQL("12345@g.us", 99)
}

func prepareMessageFixture(t *testing.T, s *Store, source string) *legacy.Bundle {
	t.Helper()
	b := prepareRouteFixture(t, s, source)
	if _, err := s.ProjectLegacyRoutes(t.Context(), b.Account, [32]byte{}, "fixture-session"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacyCompletions(t.Context(), b.Account); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLegacyMessageProjectionPreservesScopedCopiesReceiptsObservationsAndFileEvidence(t *testing.T) {
	s := testStore(t)
	b := prepareMessageFixture(t, s, legacyMessageSQL())
	result, err := s.ProjectLegacyMessages(t.Context(), b.Account, [32]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mappings != 5 || result.Observations != 5 || result.ProviderReceipts != 2 || result.MappingOnly != 3 || !result.SelfIdentity {
		t.Fatal("incomplete retained-message projection", result)
	}
	member, found, err := s.TelegramMapping(t.Context(), b.Account, "77", 123, 42)
	if err != nil || !found || member.WhatsAppMessage != "private-owner" || member.WhatsAppChat != "12025550123@c.us" {
		t.Fatal("original private member copy lost")
	}
	master, found, err := s.TelegramMapping(t.Context(), b.Account, "master", 123, 42)
	if err != nil || !found || master.WhatsAppMessage != "private-master" {
		t.Fatal("overlapping private IDs crossed bot scope")
	}
	row, err := s.queries.TelegramMapping(t.Context(), db.TelegramMappingParams{AccountID: b.Account, Scope: "*", TelegramChatID: -100, TelegramMessageID: 11})
	if err != nil || !row.Media || string(row.Receipt) != "null" || row.ReceiptOrigin != "mapping-only" || row.ThreadID != 44 || row.PlanKey.Valid {
		t.Fatal("mapping-only media fabricated receipt or frozen plan")
	}
	label, err := s.queries.TelegramMapping(t.Context(), db.TelegramMappingParams{AccountID: b.Account, Scope: "*", TelegramChatID: -100, TelegramMessageID: 10})
	if err != nil || !bytes.Contains(label.Receipt, []byte("future_field")) {
		t.Fatal("raw provider receipt fields lost")
	}
	bot, message, found, err := s.ArchivedMedia(t.Context(), b.Account, "12025550123@c.us", "private-owner")
	if err != nil || !found || bot != "77" || len(message.Photo) != 1 || message.Photo[0].FileID != "owner-77-file" {
		t.Fatal("actual bot-scoped user media observation not reusable")
	}
	if _, _, found, err := s.ArchivedMedia(t.Context(), b.Account, "12025550123@c.us", "private-master"); err != nil || found {
		t.Fatal("master copy borrowed another bot's private file ID")
	}
	var seq int64
	var cleanup string
	if err := s.pool.QueryRow(t.Context(), "SELECT seq,cleanup_error FROM telegram_observations WHERE scope='77' AND message_id=42").Scan(&seq, &cleanup); err != nil || seq != 9007199254740995 || cleanup != "original cleanup failure" {
		t.Fatal("exact observation sequence or cleanup metadata changed")
	}
	var deleted bool
	if err := s.pool.QueryRow(t.Context(), "SELECT deleted,cleanup_error FROM telegram_observations WHERE message_id=12").Scan(&deleted, &cleanup); err != nil || !deleted || cleanup != "" {
		t.Fatal("confirmed retired topic left observations active")
	}
	self, found, err := s.SelfIdentity(t.Context(), b.Account)
	if err != nil || !found || self != "12025550999@c.us" {
		t.Fatal("verified original self identity lost")
	}
	var effects, jobs int
	_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM effects").Scan(&effects)
	_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs").Scan(&jobs)
	if effects != 0 || jobs != 0 || !errors.Is(s.LegacyRuntimeReady(t.Context(), b.Account), ErrLegacyPending) {
		t.Fatal("message projection activated provider work or fabricated effects")
	}
	observation := ingress.Observation{AccountID: b.Account, Scope: "*", Bot: "master", ChatID: -100, MessageID: 10000, ThreadID: 44, Kind: "chat", Date: 1700000000123, Payload: []byte(`{"message_id":10000,"chat":{"id":-100}}`)}
	if _, err := s.Admit(t.Context(), nil, &observation); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(t.Context(), "SELECT seq FROM telegram_observations WHERE message_id=10000").Scan(&seq); err != nil || seq != 9007199254740998 {
		t.Fatal("observation sequence did not advance beyond exact source IDs")
	}
	replay, err := s.ProjectLegacyMessages(t.Context(), b.Account, [32]byte{})
	if err != nil || !reflect.DeepEqual(result, replay) {
		t.Fatal("message projection replay changed checkpoint")
	}
	var count int
	_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM telegram_observations").Scan(&count)
	if count != 6 {
		t.Fatal("projection replay reset subsequent observations")
	}
}

func TestLegacyMessageProjectionRollsBackRowsIdentityAndSequenceRestarts(t *testing.T) {
	s := testStore(t)
	b := prepareMessageFixture(t, s, legacyMessageSQL())
	_, err := s.pool.Exec(t.Context(), `CREATE FUNCTION reject_message_projection() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END; $$;
CREATE TRIGGER reject_message_projection BEFORE INSERT ON legacy_message_projections FOR EACH ROW EXECUTE FUNCTION reject_message_projection();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacyMessages(t.Context(), b.Account, [32]byte{}); err == nil {
		t.Fatal("failed final checkpoint committed")
	}
	for _, table := range []string{"message_mappings", "telegram_observations", "whatsapp_self", "legacy_message_projections"} {
		var count int
		if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial message projection survived", table)
		}
	}
	tx, err := s.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"message_mappings", "telegram_observations"} {
		next, err := identitySequenceNext(t.Context(), tx, table)
		if err != nil || next != 1 {
			t.Fatal("failed import changed identity sequence", table, err)
		}
	}
	rollback(tx)
	if _, err := s.pool.Exec(t.Context(), "DROP TRIGGER reject_message_projection ON legacy_message_projections"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacyMessages(t.Context(), b.Account, [32]byte{}); err != nil {
		t.Fatal("rolled back message import could not retry", err)
	}
}

func TestLegacyMessageProjectionRefusesMissingLocationAndNeverRewindsCounters(t *testing.T) {
	t.Run("unlocated group copy", func(t *testing.T) {
		s := testStore(t)
		b := prepareMessageFixture(t, s, `INSERT INTO messages VALUES('12345@g.us','unlocated','-100',42,'master','in',1);`)
		if _, err := s.ProjectLegacyMessages(t.Context(), b.Account, [32]byte{}); !errors.Is(err, legacy.ErrMappingEvidence) {
			t.Fatal("unlocated group copy borrowed a current route thread")
		}
	})
	t.Run("existing sequence gap", func(t *testing.T) {
		s := testStore(t)
		b := prepareMessageFixture(t, s, `INSERT INTO messages VALUES('12025550123@c.us','private-copy','123',42,'77','in',1);`)
		if _, err := s.pool.Exec(t.Context(), "SELECT nextval(pg_get_serial_sequence('message_mappings','seq')) FROM generate_series(1,7)"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ProjectLegacyMessages(t.Context(), b.Account, [32]byte{}); err != nil {
			t.Fatal(err)
		}
		var seq int64
		_ = s.pool.QueryRow(t.Context(), "SELECT seq FROM message_mappings").Scan(&seq)
		if seq != 8 {
			t.Fatal("existing identity counter rewound")
		}
	})
}

func legacyConsumerJob(t *testing.T, s *Store, key, event string, data map[string]any) delivery.Job {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"event": event, "accountId": "fixture-account", "sessionId": "fixture-session", "idempotencyKey": key, "data": data})
	if accepted, err := s.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: "ow:" + key, Stream: "wa:12345@g.us", Kind: "wa", Payload: raw}); err != nil || !accepted {
		t.Fatal(err)
	}
	return claim(t, s)
}

func TestLegacyRetainedCopiesFeedNativeEditsReactionsAndRevocationChoices(t *testing.T) {
	s := testStore(t)
	b := prepareMessageFixture(t, s, legacyMessageSQL())
	if _, err := s.ProjectLegacyMessages(t.Context(), b.Account, [32]byte{}); err != nil {
		t.Fatal(err)
	}
	// This fixture exercises consumers after simulated completion of unrelated
	// import stages. Production projection APIs never activate an account.
	if _, err := s.pool.Exec(t.Context(), "UPDATE legacy_imports SET activated_at=now()"); err != nil {
		t.Fatal(err)
	}
	job := legacyConsumerJob(t, s, "legacy-edit", "message.edited", map[string]any{"chatId": "12345@g.us", "messageId": "group-source", "body": "Edited caption"})
	edit, err := s.EditInput(t.Context(), job, changes.Settings{Session: "fixture-session", OwnerID: 123, Management: -100, NoticeTTL: 60})
	if err != nil || len(edit.Destinations) != 2 || edit.Destinations[0].Caption || !edit.Destinations[1].Caption || edit.Destinations[1].MessageID != 11 || edit.Destinations[1].Format.Name != "Original linked name" || len(edit.MentionedIDs) != 1 {
		t.Fatal("imported caption/format/mention evidence not used by native edit input", err)
	}
	finish(t, s, job)
	job = legacyConsumerJob(t, s, "legacy-reaction", "message.reaction", map[string]any{"chatId": "12345@g.us", "messageId": "group-source", "senderId": "999999@lid", "reaction": "👍"})
	reaction, err := s.ReactionInput(t.Context(), job, reactions.Settings{Session: "fixture-session", OwnerID: 123, NoticeTTL: 60})
	if err != nil || len(reaction.Destinations) != 1 || reaction.Destinations[0].MessageID != 11 {
		t.Fatal("mapping-only media was not the retained legacy reaction target", err)
	}
	finish(t, s, job)
	job = legacyConsumerJob(t, s, "legacy-revocation", "message.revoked", map[string]any{"chatId": "12345@g.us", "messageId": "group-source"})
	if err := s.AdmitRevocation(t.Context(), job, revocations.Settings{Session: "fixture-session", OwnerID: 123, ControlTTL: 600, NoticeTTL: 60}); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := s.pool.QueryRow(t.Context(), "SELECT payload FROM revocation_tasks").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var task revocations.Task
	if json.Unmarshal(raw, &task) != nil || !task.Own || task.Status != "pending" || task.Primary.MessageID != 11 || !task.Primary.Media || len(task.Copies) != 2 {
		t.Fatal("original own-message choice or primary media copy lost")
	}
}

func TestLegacyRetainedReactionUsesNewestCopyAfterOlderMedia(t *testing.T) {
	s := testStore(t)
	source := legacyMessageSQL() + `
INSERT INTO messages VALUES('12345@g.us','group-source','-100',13,'master','in',0);
INSERT INTO telegram_messages VALUES(9007199254740998,'*','-100',13,'master',44,'chat',1700000000123,0,0,NULL);
`
	b := prepareMessageFixture(t, s, source)
	if _, err := s.ProjectLegacyMessages(t.Context(), b.Account, [32]byte{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(t.Context(), "UPDATE legacy_imports SET activated_at=now()"); err != nil {
		t.Fatal(err)
	}
	job := legacyConsumerJob(t, s, "newest-legacy-copy", "message.reaction", map[string]any{"chatId": "12345@g.us", "messageId": "group-source", "senderId": "999999@lid", "reaction": "👍"})
	input, err := s.ReactionInput(t.Context(), job, reactions.Settings{Session: "fixture-session", OwnerID: 123, NoticeTTL: 60})
	if err != nil || len(input.Destinations) != 1 || input.Destinations[0].MessageID != 13 {
		t.Fatal("newest legacy text copy displaced by an older attachment", err)
	}
}
