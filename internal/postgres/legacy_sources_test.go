package postgres

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/bots"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/reactions"
)

func TestLegacySourcesPreserveParticipantContextWithoutInventingContent(t *testing.T) {
	s := testStore(t)
	source := legacyMessageSQL() + legacyValueSQL("wamessage", "12345@g.us:participant-source", `{"sender":"888888:2@lid","name":"Original participant","fromMe":false,"mentionedIds":[]}`) + legacyValueSQL("contact", "12345@g.us", `{"name":"Legacy group display cache"}`)
	source += `INSERT INTO messages VALUES('12345@g.us','participant-source','-100',15,'master','in',0);
INSERT INTO telegram_messages VALUES(9007199254740998,'*','-100',15,'master',44,'chat',1700000000123,0,0,NULL);`
	b := prepareMessageFixture(t, s, source)
	if _, err := s.ProjectLegacyMessages(t.Context(), b.Account, [32]byte{}); err != nil {
		t.Fatal(err)
	}
	result, err := s.ProjectLegacySources(t.Context(), b.Account)
	if err != nil || result.Sources != 2 {
		t.Fatal("source identity projection failed", err)
	}
	raw, err := s.queries.ProvisionMessageSender(t.Context(), db.ProvisionMessageSenderParams{AccountID: b.Account, WhatsappChat: "12345@g.us", MessageID: "participant-source"})
	var sender identity.Source
	if err != nil || json.Unmarshal(raw, &sender) != nil || sender.SenderID != "888888@lid" || sender.Name != "Original participant" {
		t.Fatal("old copy lost participant identity")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if _, exists := fields["Body"]; exists {
		t.Fatal("identity-only source invented body content")
	}
	replay, err := s.ProjectLegacySources(t.Context(), b.Account)
	if err != nil || !reflect.DeepEqual(result, replay) || !errors.Is(s.LegacyRuntimeReady(t.Context(), b.Account), ErrLegacyPending) {
		t.Fatal("source replay changed state or activated import")
	}
	// Exercise the ordinary owner command after this isolated fixture marks
	// its unrelated activation stages complete. No provider is contacted.
	if _, err := s.pool.Exec(t.Context(), "UPDATE legacy_imports SET activated_at=now()"); err != nil {
		t.Fatal(err)
	}
	m := ownerText(200, -100, "/create")
	m.MessageThreadID = 44
	m.IsTopicMessage = true
	m.ReplyToMessage = &models.Message{ID: 15, Chat: models.Chat{ID: -100}, MessageThreadID: 44}
	u := models.Update{ID: 200, Message: m}
	payload, _ := json.Marshal(map[string]any{"bot": "master", "update": u})
	if accepted, err := s.Enqueue(t.Context(), delivery.Event{AccountID: b.Account, Dedup: "tg:master:200", Stream: "tg:-100:44", Kind: "tg", Payload: payload}); err != nil || !accepted {
		t.Fatal(err)
	}
	job := claim(t, s)
	plan, err := s.PrepareProvisionControl(t.Context(), job, "master_fixture", bots.Settings{Session: "fixture-session", OwnerID: 123, Management: -100, CommandTTL: 60, ControlTTL: 600})
	if err != nil || len(plan.Packets) != 1 || plan.Packets[0].Markup == nil || !strings.Contains(plan.Packets[0].Text, "Create this contact’s bot") {
		t.Fatal("reply/create treated a retained participant as missing", err)
	}
	var person string
	if err := s.pool.QueryRow(t.Context(), "SELECT person_id FROM contact_provisions").Scan(&person); err != nil || person != "888888@lid" {
		t.Fatal("participant display number became a phone binding")
	}
}

func TestLegacySourceProjectionRollsBackAndKeepsHarmlessUnboundCaches(t *testing.T) {
	s := testStore(t)
	b := prepareMessageFixture(t, s, legacyMessageSQL()+legacyValueSQL("contact", "unknown", `{"name":"Legacy unknown sender"}`))
	if _, err := s.ProjectLegacyMessages(t.Context(), b.Account, [32]byte{}); err != nil {
		t.Fatal(err)
	}
	_, err := s.pool.Exec(t.Context(), `CREATE FUNCTION reject_source_projection() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END; $$;
CREATE TRIGGER reject_source_projection BEFORE INSERT ON legacy_source_projections FOR EACH ROW EXECUTE FUNCTION reject_source_projection();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacySources(t.Context(), b.Account); err == nil {
		t.Fatal("failed source checkpoint committed")
	}
	var count int
	_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM retained_message_sources").Scan(&count)
	if count != 0 {
		t.Fatal("partial source identity survived rollback")
	}
	if _, err := s.pool.Exec(t.Context(), "DROP TRIGGER reject_source_projection ON legacy_source_projections"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacySources(t.Context(), b.Account); err != nil {
		t.Fatal(err)
	}
}

func TestOwnReactionEchoUsesProvenGroupActorWithoutTreatingPrivatePeerAsSelf(t *testing.T) {
	s := testStore(t)
	source := legacyMessageSQL() + legacyValueSQL("wamessage", "12345@g.us:own-group-proof", `{"sender":"888888@lid","name":"Self on another device","fromMe":true,"linkSender":"12025550999@c.us"}`) +
		legacyValueSQL("wamessage", "777777@lid:own-private-copy", `{"sender":"777777@lid","name":"Private peer","fromMe":true,"linkSender":"12025550999@c.us"}`)
	b := prepareMessageFixture(t, s, source)
	if _, err := s.ProjectLegacyMessages(t.Context(), b.Account, [32]byte{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacySources(t.Context(), b.Account); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		account, id string
		expected    bool
	}{{b.Account, "888888@lid", true}, {b.Account, "777777@lid", false}, {"other-account", "888888@lid", false}} {
		own, err := s.queries.SourceActorIsOwn(t.Context(), db.SourceActorIsOwnParams{AccountID: tc.account, Identities: []string{tc.id}})
		if err != nil || own.Bool != tc.expected {
			t.Fatal("own actor proof escaped source/account scope")
		}
	}
	if _, err := s.pool.Exec(t.Context(), "UPDATE legacy_imports SET activated_at=now()"); err != nil {
		t.Fatal(err)
	}
	echo := reactions.Echo{ActorID: "12025550999@c.us", Emoji: "👍", Target: control.Target{BotID: "master", ChatID: -100, ThreadID: 44}, MessageID: 11, At: time.Now().UnixMilli()}
	raw, _ := json.Marshal(echo)
	if err := s.queries.SaveReactionEcho(t.Context(), db.SaveReactionEchoParams{AccountID: b.Account, WhatsappChat: "12345@g.us", MessageID: "group-source", Payload: raw}); err != nil {
		t.Fatal(err)
	}
	job := legacyConsumerJob(t, s, "proven-own-lid-echo", "message.reaction", map[string]any{"chatId": "12345@g.us", "messageId": "group-source", "senderId": "888888@lid", "reaction": "👍"})
	input, err := s.ReactionInput(t.Context(), job, reactions.Settings{Session: "fixture-session", OwnerID: 123, NoticeTTL: 60})
	if err != nil || input.State.Actors["888888@lid"].Echo == nil || input.State.Actors["888888@lid"].Echo.ActorID != "888888@lid" {
		t.Fatal("proven own LID echo was not recognized without a phone alias", err)
	}
	finish(t, s, job)
	job = legacyConsumerJob(t, s, "private-peer-is-not-self", "message.reaction", map[string]any{"chatId": "12345@g.us", "messageId": "group-source", "senderId": "777777@lid", "reaction": "👍"})
	input, err = s.ReactionInput(t.Context(), job, reactions.Settings{Session: "fixture-session", OwnerID: 123, NoticeTTL: 60})
	if err != nil || input.State.Actors["777777@lid"].Echo != nil {
		t.Fatal("outgoing private copy falsely proved the peer was self", err)
	}
}

func TestOwnActorProofAlsoReadsNativeSnapshotsAndExcludesPrivateRecipients(t *testing.T) {
	s := testStore(t)
	for _, tc := range []struct {
		chat, actor string
		expected    bool
	}{{"12345@g.us", "666666@lid", true}, {"777777@lid", "777777@lid", false}} {
		enqueue(t, s, "own-proof:"+tc.chat, "own-proof:"+tc.chat)
		job := claim(t, s)
		raw, _ := json.Marshal(identity.Source{ChatID: tc.chat, MessageID: "source", SenderID: tc.actor, Own: true})
		if err := s.queries.SaveWhatsAppSource(t.Context(), db.SaveWhatsAppSourceParams{AccountID: "fixture-account", JobID: job.ID, WhatsappChat: tc.chat, Payload: raw}); err != nil {
			t.Fatal(err)
		}
		own, err := s.queries.SourceActorIsOwn(t.Context(), db.SourceActorIsOwnParams{AccountID: "fixture-account", Identities: []string{tc.actor}})
		if err != nil || own.Bool != tc.expected {
			t.Fatal("native own-actor proof confused source sender and private recipient")
		}
	}
}
