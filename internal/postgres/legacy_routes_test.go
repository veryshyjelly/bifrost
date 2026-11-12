package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func legacyRetiredSQL(chat string, thread int64) string {
	raw, _ := json.Marshal([]any{"-100", thread})
	sum := sha256.Sum256(raw)
	value, _ := json.Marshal(map[string]string{"chatId": chat})
	return "INSERT INTO kv VALUES('retired-topic','" + hex.EncodeToString(sum[:]) + "','" + string(value) + "');"
}

func prepareRouteFixture(t *testing.T, s *Store, source string) *legacy.Bundle {
	t.Helper()
	b := prepareDirectoryFixture(t, s, legacyDirectorySQL()+source, true)
	if _, err := s.ProjectLegacyDirectory(t.Context(), b.Account); err != nil {
		t.Fatal(err)
	}
	return b
}

func legacyRouteSQL() string {
	return `
INSERT INTO kv VALUES('route','999999@lid','{"chatId":"999999@lid","name":"Inbox name","tgChat":"-100","thread":7,"mode":"inbox","topicSyncedName":"Previous topic name"}');
INSERT INTO kv VALUES('route','12025550124@c.us','{"chatId":"12025550124@c.us","name":"Private bot","tgChat":"-100","mode":"dedicated","inboxRetired":true}');
INSERT INTO kv VALUES('route','111@g.us','{"chatId":"111@g.us","name":"Mirror group","tgChat":"-200","mode":"group","inbox":{"chat":"-100","thread":9},"inboxReturnKey":"original-return"}');
INSERT INTO kv VALUES('route','222@g.us','{"chatId":"222@g.us","name":"Muted mirror","tgChat":-300,"mode":"muted","inbox":{"chat":"-100","thread":11}}');
INSERT INTO kv VALUES('route','12025550125@c.us','{"chatId":"12025550125@c.us","name":"Ignored","tgChat":"-100","thread":15,"mode":"ignored"}');
INSERT INTO effects VALUES('original-topic:topic','done','{"message_thread_id":99,"name":"Former topic"}');
` + legacyRetiredSQL("222@g.us", 11) + legacyRetiredSQL("333@g.us", 99)
}

func TestLegacyRouteProjectionPreservesEveryModeInboxGenerationAndPermanentReservation(t *testing.T) {
	s := testStore(t)
	b := prepareRouteFixture(t, s, legacyRouteSQL())
	result, err := s.ProjectLegacyRoutes(t.Context(), b.Account, [32]byte{}, "planned-provider-connection")
	if err != nil {
		t.Fatal(err)
	}
	if result.Routes != 5 || result.Inboxes != 5 || result.Retired != 2 || result.PendingRetirements != 0 || result.Session != "planned-provider-connection" {
		t.Fatal("incomplete route projection")
	}
	for id, mode := range map[string]conversation.Mode{"12025550123@c.us": conversation.Inbox, "12025550124@c.us": conversation.Dedicated, "111@g.us": conversation.Group, "222@g.us": conversation.Muted, "12025550125@c.us": conversation.Ignored} {
		route, found, err := s.ConversationRoute(t.Context(), b.Account, id)
		if err != nil || !found || route.Mode != mode {
			t.Fatal("route mode or verified canonical identity lost", id)
		}
		if mode == conversation.Dedicated && (!route.InboxRetired || route.ThreadID != 0) {
			t.Fatal("retired private inbox resurrected")
		}
	}
	if generation, err := s.queries.MirrorReturnKey(t.Context(), db.MirrorReturnKeyParams{AccountID: b.Account, WhatsappChat: "111@g.us"}); err != nil || generation != "original-return" {
		t.Fatal("mirror return generation changed")
	}
	name, err := s.queries.MetadataTopicName(t.Context(), db.MetadataTopicNameParams{AccountID: b.Account, SessionID: result.Session, SourceID: "12025550123@c.us", ChatID: -100, ThreadID: 7})
	if err != nil || name != "Previous topic name" {
		t.Fatal("original topic-name cache lost or wrong connection scope")
	}
	for _, thread := range []int64{11, 99} {
		topic, err := s.queries.RetiredTopic(t.Context(), db.RetiredTopicParams{AccountID: b.Account, TelegramChatID: -100, ThreadID: thread})
		if err != nil || !topic.Deleted {
			t.Fatal("permanent retired topic dropped")
		}
		if err := s.PutIngressRoute(t.Context(), b.Account, ingress.Route{WhatsAppChat: "777@g.us", TelegramChatID: -100, ThreadID: thread}); !errors.Is(err, conversation.ErrInboxRequired) {
			t.Fatal("retired topic reused by another conversation")
		}
	}
	if !errors.Is(s.LegacyRuntimeReady(t.Context(), b.Account), ErrLegacyPending) {
		t.Fatal("route projection activated account")
	}
	// Repeated projection preserves later native route and generation commits.
	if _, err := s.pool.Exec(t.Context(), "UPDATE bridge_routes SET name='New native route',telegram_chat_id=-400,thread_id=0 WHERE whatsapp_chat='111@g.us'"); err != nil {
		t.Fatal(err)
	}
	if err := s.queries.SaveMirrorReturn(t.Context(), db.SaveMirrorReturnParams{AccountID: b.Account, WhatsappChat: "111@g.us", ReturnKey: "new-native-generation"}); err != nil {
		t.Fatal(err)
	}
	replay, err := s.ProjectLegacyRoutes(t.Context(), b.Account, [32]byte{}, result.Session)
	route, _, readErr := s.ConversationRoute(t.Context(), b.Account, "111@g.us")
	if err != nil || readErr != nil || !reflect.DeepEqual(result, replay) || route.Name != "New native route" || route.TelegramChatID != -400 {
		t.Fatal("projection replay reset subsequent routing")
	}
	if _, err := s.ProjectLegacyRoutes(t.Context(), b.Account, [32]byte{}, "different-planned-connection"); !errors.Is(err, ErrLegacyDestination) {
		t.Fatal("projection replay changed frozen connection")
	}
}

func TestLegacyRouteProjectionRecoversOpaqueReservationsOnlyFromRetainedEvidence(t *testing.T) {
	receiptKeyRaw, _ := json.Marshal([]any{"master", "-100", 42})
	receiptKey := sha256.Sum256(receiptKeyRaw)
	for name, evidence := range map[string]string{
		"confirmed topic":        `INSERT INTO effects VALUES('topic:topic','done','{"message_thread_id":99,"name":"Original"}');`,
		"observation":            `INSERT INTO telegram_messages VALUES(1,'*','-100',42,'master',99,'chat',1700000000000,0,1,NULL);`,
		"cached native receipt":  `INSERT INTO kv VALUES('telegram-receipt','` + hex.EncodeToString(receiptKey[:]) + `','{"message_id":42,"chat":{"id":-100},"message_thread_id":99}');`,
		"frozen target":          `INSERT INTO kv VALUES('delivery-target','original-target','{"bot":"master","destination":"-100","thread":99}');`,
		"saved feed coordinates": `INSERT INTO kv VALUES('status-feed','` + legacyDirectoryScopeForPG("fixture-account") + `','{"chatId":"status@broadcast","tgChat":"-100","thread":99,"mode":"status"}');`,
	} {
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			b := prepareRouteFixture(t, s, evidence+legacyRetiredSQL("333@g.us", 99))
			r, err := s.ProjectLegacyRoutes(t.Context(), b.Account, [32]byte{}, "")
			if err != nil || r.Retired != 1 || r.Session != "source-session" {
				t.Fatal("exact source reservation not recovered", err)
			}
			var effects int
			_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM effects").Scan(&effects)
			if effects != 0 {
				t.Fatal("coordinate recovery fabricated a native effect receipt")
			}
		})
	}
	for _, evidence := range []string{"", `INSERT INTO effects VALUES('topic:topic','uncertain','{"message_thread_id":99,"name":"Uncertain"}');`} {
		s := testStore(t)
		b := prepareRouteFixture(t, s, evidence+legacyRetiredSQL("333@g.us", 99))
		if _, err := s.ProjectLegacyRoutes(t.Context(), b.Account, [32]byte{}, ""); !errors.Is(err, legacy.ErrTopicEvidence) {
			t.Fatal("missing or uncertain creation adopted as exact topic evidence")
		}
		var count int
		_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM retired_topics").Scan(&count)
		if count != 0 {
			t.Fatal("unresolved reservation partially projected")
		}
	}
}

func TestLegacyRouteProjectionKeepsOwnedRetirementCrashFenced(t *testing.T) {
	s := testStore(t)
	b := prepareRouteFixture(t, s, `INSERT INTO kv VALUES('route','111@g.us','{"chatId":"111@g.us","tgChat":"-100","thread":7,"mode":"inbox"}');`+legacyRetiredSQL("111@g.us", 7))
	result, err := s.ProjectLegacyRoutes(t.Context(), b.Account, [32]byte{}, "")
	if err != nil || result.PendingRetirements != 1 {
		t.Fatal("owned retirement crash lost", err)
	}
	route, _, err := s.ConversationRoute(t.Context(), b.Account, "111@g.us")
	if err != nil || route.ThreadID != 7 || route.InboxRetired {
		t.Fatal("source route rewritten before its cleanup continuation")
	}
	if err := s.PutIngressRoute(t.Context(), b.Account, ingress.Route{WhatsAppChat: "222@g.us", TelegramChatID: -100, ThreadID: 7}); !errors.Is(err, conversation.ErrInboxRequired) {
		t.Fatal("retirement fence lost")
	}
}

func TestLegacyRouteProjectionRollbackAndPrerequisites(t *testing.T) {
	t.Run("final checkpoint rollback", func(t *testing.T) {
		s := testStore(t)
		b := prepareRouteFixture(t, s, legacyRouteSQL())
		_, err := s.pool.Exec(t.Context(), `CREATE FUNCTION reject_route_projection() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END; $$;
CREATE TRIGGER reject_route_projection BEFORE INSERT ON legacy_route_projections FOR EACH ROW EXECUTE FUNCTION reject_route_projection();`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.ProjectLegacyRoutes(t.Context(), b.Account, [32]byte{}, ""); err == nil {
			t.Fatal("failed route checkpoint committed")
		}
		for _, table := range []string{"bridge_routes", "conversation_inboxes", "retired_topics", "mirror_returns", "metadata_topic_names", "legacy_route_projections"} {
			var count int
			if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
				t.Fatal("partial route projection survived", table)
			}
		}
		if _, err := s.pool.Exec(t.Context(), "DROP TRIGGER reject_route_projection ON legacy_route_projections"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ProjectLegacyRoutes(t.Context(), b.Account, [32]byte{}, ""); err != nil {
			t.Fatal(err)
		}
	})
	for _, scenario := range []string{"missing directory", "native route conflict", "alias collision"} {
		t.Run(scenario, func(t *testing.T) {
			s := testStore(t)
			source := legacyRouteSQL()
			if scenario == "alias collision" {
				source += `INSERT INTO kv VALUES('route','12025550123@c.us','{"chatId":"12025550123@c.us","tgChat":"-100","thread":8,"mode":"inbox"}');`
			}
			b := prepareDirectoryFixture(t, s, legacyDirectorySQL()+source, true)
			if scenario != "missing directory" {
				if _, err := s.ProjectLegacyDirectory(t.Context(), b.Account); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "native route conflict" {
				if err := s.PutIngressRoute(t.Context(), b.Account, ingress.Route{WhatsAppChat: "777@g.us", TelegramChatID: -400}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.ProjectLegacyRoutes(t.Context(), b.Account, [32]byte{}, ""); err == nil {
				t.Fatal("unsafe route projection accepted")
			}
			var count int
			_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM retired_topics").Scan(&count)
			if count != 0 {
				t.Fatal("failed projection changed reservations")
			}
			if scenario == "native route conflict" {
				r, found, err := s.ConversationRoute(t.Context(), b.Account, "777@g.us")
				if err != nil || !found || r.TelegramChatID != -400 {
					t.Fatal("existing native route overwritten")
				}
			}
		})
	}
}
