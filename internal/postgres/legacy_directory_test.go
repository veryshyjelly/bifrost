package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/mentions"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func legacyDirectoryScopeForPG(account string) string {
	sum := sha256.Sum256([]byte(account))
	return hex.EncodeToString(sum[:])
}

func prepareDirectoryFixture(t *testing.T, s *Store, extra string, projectBots bool) *legacy.Bundle {
	t.Helper()
	b, _ := legacyPreparationBundle(t, extra)
	if _, err := s.PrepareLegacyImport(t.Context(), b, legacy.SQLiteOptions{}); err != nil {
		t.Fatal(err)
	}
	if projectBots {
		if _, err := s.ProjectLegacyBots(t.Context(), b.Account, [32]byte{}); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

func legacyDirectorySQL() string {
	scope := legacyDirectoryScopeForPG("fixture-account")
	return `
INSERT INTO aliases VALUES('999999:2@lid','12025550123@s.whatsapp.net');
INSERT INTO kv VALUES('contact','12025550123@c.us','{"name":"Canonical profile"}');
INSERT INTO kv VALUES('contact','999999@lid','{"name":"Selected bound profile","number":"999999","bot":"77","metadataSyncedAt":1700000000000}');
INSERT INTO kv VALUES('contact','888888@lid','{"name":"Unresolved contact","number":"12025550999"}');
INSERT INTO kv VALUES('mention-token','aabbccdd','{"session":"` + scope + `","chat":"12345@g.us","person":"999999@lid"}');
INSERT INTO kv VALUES('sync-group','` + scope + `:12345@g.us','{"name":"Original roster","fetchedAt":1700000000123,"members":[{"id":"999999@lid","canonical":"999999@c.us","name":"Cached member","isAdmin":true}]}');
`
}

func TestLegacyDirectoryProjectionPreservesNamesAliasesAndScopedMentionEvidence(t *testing.T) {
	s := testStore(t)
	b := prepareDirectoryFixture(t, s, legacyDirectorySQL(), true)
	result, err := s.ProjectLegacyDirectory(t.Context(), b.Account)
	if err != nil {
		t.Fatal(err)
	}
	if result.Aliases != 1 || result.Profiles != 3 || result.Bindings != 1 || result.Tokens != 1 || result.Rosters != 1 || result.ArchiveDigest != b.Digest {
		t.Fatal("incomplete directory projection")
	}
	directory, err := s.IdentityDirectory(t.Context(), b.Account)
	if err != nil {
		t.Fatal(err)
	}
	person := directory.Person("12025550123@c.us", nil)
	if person.Name != "Selected bound profile" || person.Bot.ID != "77" || !person.Bot.Started || directory.Canonical("888888@lid") != "888888@lid" {
		t.Fatal("selected bound name lost or display number inferred as phone evidence")
	}
	var canonical, sourceProfile string
	if err := s.pool.QueryRow(t.Context(), "SELECT contact_id,source_profile FROM bridge_contact_bots").Scan(&canonical, &sourceProfile); err != nil || canonical != "12025550123@c.us" || sourceProfile != "999999@lid" {
		t.Fatal("routing identity merged with selected name source")
	}
	token, found, err := s.MentionToken(t.Context(), b.Account, "aabbccdd")
	if err != nil || !found || token.ChatID != "12345@g.us" || token.PersonID != "999999@lid" {
		t.Fatal("original copied mention token lost")
	}
	if _, found, err := s.MentionToken(t.Context(), "other-account", "aabbccdd"); err != nil || found {
		t.Fatal("copied token escaped account scope")
	}
	roster, found, err := s.MentionRoster(t.Context(), b.Account, "12345@g.us")
	if err != nil || !found || roster.FetchedAt != 1700000000123 || roster.Cached || len(roster.Members) != 1 || roster.Members[0].ID != "999999@lid" || !roster.Members[0].IsAdmin {
		t.Fatal("original roster scope/evidence changed")
	}
	if !errors.Is(s.LegacyRuntimeReady(t.Context(), b.Account), ErrLegacyPending) {
		t.Fatal("directory projection activated import")
	}
	// Unconfirmed source observation must not replace the bound cached name.
	if err := s.queries.SaveContactProfile(t.Context(), db.SaveContactProfileParams{AccountID: b.Account, PersonID: canonical, Name: "Unconfirmed source display"}); err != nil {
		t.Fatal(err)
	}
	directory, err = s.IdentityDirectory(t.Context(), b.Account)
	if err != nil || directory.Person(canonical, nil).Name != "Selected bound profile" {
		t.Fatal("unconfirmed profile displaced selected source name")
	}
	// Confirmed native metadata supersedes the imported name preference.
	if err := s.queries.SaveContactProfile(t.Context(), db.SaveContactProfileParams{AccountID: b.Account, PersonID: canonical, Name: "Confirmed native profile", MetadataSynced: true}); err != nil {
		t.Fatal(err)
	}
	directory, err = s.IdentityDirectory(t.Context(), b.Account)
	if err != nil || directory.Person(canonical, nil).Name != "Confirmed native profile" {
		t.Fatal("confirmed metadata could not refresh imported bound name")
	}
	newer := mentions.Roster{ChatID: roster.ChatID, Name: "Newer native roster", FetchedAt: roster.FetchedAt + 1, Members: []identity.Member{}}
	payload, _ := json.Marshal(newer)
	if err := s.queries.SaveMentionRoster(t.Context(), db.SaveMentionRosterParams{AccountID: b.Account, ChatID: newer.ChatID, Payload: payload, FetchedAt: newer.FetchedAt}); err != nil {
		t.Fatal(err)
	}
	replay, err := s.ProjectLegacyDirectory(t.Context(), b.Account)
	if err != nil || !reflect.DeepEqual(result, replay) {
		t.Fatal("projection replay changed its original result")
	}
	after, _, err := s.MentionRoster(t.Context(), b.Account, newer.ChatID)
	directory, dirErr := s.IdentityDirectory(t.Context(), b.Account)
	if err != nil || dirErr != nil || after.Name != newer.Name || directory.Person(canonical, nil).Name != "Confirmed native profile" {
		t.Fatal("projection replay reset subsequent native state")
	}
}

func TestLegacyDirectoryProjectionRequiresBotPrerequisiteAndRejectsNativeConflict(t *testing.T) {
	for _, scenario := range []string{"missing bots", "existing native profile", "foreign token"} {
		t.Run(scenario, func(t *testing.T) {
			s := testStore(t)
			source := legacyDirectorySQL()
			if scenario == "foreign token" {
				source = strings.ReplaceAll(source, legacyDirectoryScopeForPG("fixture-account"), legacyDirectoryScopeForPG("other-account"))
			}
			b := prepareDirectoryFixture(t, s, source, scenario != "missing bots")
			if scenario == "existing native profile" {
				if err := s.queries.SaveContactProfile(t.Context(), db.SaveContactProfileParams{AccountID: b.Account, PersonID: "12025550124@c.us", Name: "Existing native profile"}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.ProjectLegacyDirectory(t.Context(), b.Account); err == nil {
				t.Fatal("unsafe directory projection accepted")
			}
			var bindings, aliases int
			_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM bridge_contact_bots").Scan(&bindings)
			_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM contact_aliases").Scan(&aliases)
			if bindings != 0 || aliases != 0 {
				t.Fatal("failed projection changed native identity state")
			}
		})
	}
}

func TestLegacyDirectoryProjectionRollsBackEveryIdentityWriteOnRosterFailure(t *testing.T) {
	s := testStore(t)
	b := prepareDirectoryFixture(t, s, legacyDirectorySQL(), true)
	_, err := s.pool.Exec(t.Context(), `CREATE FUNCTION reject_imported_roster() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END; $$;
CREATE TRIGGER reject_imported_roster BEFORE INSERT ON mention_rosters FOR EACH ROW EXECUTE FUNCTION reject_imported_roster();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacyDirectory(t.Context(), b.Account); err == nil {
		t.Fatal("failed roster projection committed")
	}
	for _, table := range []string{"contact_aliases", "contact_profiles", "bridge_contact_bots", "mention_tokens", "mention_rosters", "legacy_directory_projections"} {
		var count int
		if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial directory projection survived rollback", table)
		}
	}
	var credentials int
	_ = s.pool.QueryRow(t.Context(), "SELECT count(*) FROM telegram_credentials").Scan(&credentials)
	if credentials != 2 {
		t.Fatal("failed directory projection changed prior credential checkpoint")
	}
	if _, err := s.pool.Exec(t.Context(), "DROP TRIGGER reject_imported_roster ON mention_rosters"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectLegacyDirectory(t.Context(), b.Account); err != nil {
		t.Fatal("rolled back directory projection could not retry", err)
	}
}
