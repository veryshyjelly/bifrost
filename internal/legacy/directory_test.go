package legacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/identity"
)

func legacyDirectoryScope(account string) string {
	sum := sha256.Sum256([]byte(account))
	return hex.EncodeToString(sum[:])
}

func TestProjectDirectoryPreservesBoundNamesCopiedTokensAndRosterWithoutGuessingPhones(t *testing.T) {
	scope := legacyDirectoryScope("original-account")
	token, _ := json.Marshal(map[string]string{"session": scope, "chat": "12345@g.us", "person": "999999@lid"})
	values := []KV{
		{Kind: "contact", ID: "12025550123@c.us", Value: `{"name":"Canonical profile","number":"12025550123"}`},
		{Kind: "contact", ID: "999999@lid", Value: `{"name":"Selected bound profile","number":"999999","bot":"77","metadataSyncedAt":1700000000000}`},
		{Kind: "contact", ID: "888888@lid", Value: `{"name":"Unresolved person","number":"12025550999"}`},
		{Kind: "mention-token", ID: "aabbccdd", Value: string(token)},
		{Kind: "sync-group", ID: scope + ":12345@g.us", Value: `{"name":"Original roster","fetchedAt":1700000000123,"members":[{"id":"999999@lid","canonical":"999999@c.us","name":"Cached roster name","isAdmin":true,"isSuperAdmin":false}]}`},
	}
	result, err := ProjectDirectory("original-account", values, []Alias{{Alias: "999999:2@lid", Canonical: "12025550123@s.whatsapp.net"}}, []identity.Bot{{ID: "77", Username: "fixture_bot", Started: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Aliases) != 1 || result.Aliases[0].ID != "999999@lid" || result.Aliases[0].Canonical != "12025550123@c.us" || len(result.Bindings) != 1 || result.Bindings[0].PersonID != "12025550123@c.us" || result.Bindings[0].SourceProfile != "999999@lid" {
		t.Fatal("explicit aliases or selected binding source lost")
	}
	bindings := []identity.Binding{{PersonID: result.Bindings[0].SourceProfile, BotID: result.Bindings[0].BotID}}
	directory, err := identity.New(result.Aliases, result.Profiles, []identity.Bot{{ID: "77", Username: "fixture_bot", Started: true}}, bindings)
	if err != nil {
		t.Fatal(err)
	}
	if directory.Person("12025550123@c.us", nil).Name != "Selected bound profile" || directory.Canonical("888888@lid") != "888888@lid" {
		t.Fatal("bound name changed or display number inferred as phone evidence")
	}
	if result.Tokens["aabbccdd"].PersonID != "999999@lid" || result.Tokens["aabbccdd"].ChatID != "12345@g.us" {
		t.Fatal("copied token changed")
	}
	if len(result.Rosters) != 1 || result.Rosters[0].FetchedAt != 1700000000123 || result.Rosters[0].Cached || len(result.Rosters[0].Members) != 1 || result.Rosters[0].Members[0].ID != "999999@lid" || !result.Rosters[0].Members[0].IsAdmin {
		t.Fatal("original roster evidence changed")
	}
}

func TestProjectDirectoryRejectsForeignScopeIncompleteRostersAndConflictingBindings(t *testing.T) {
	scope := legacyDirectoryScope("original-account")
	for _, values := range [][]KV{
		{{Kind: "mention-token", ID: "aabbccdd", Value: `{"session":"other-account","chat":"12345@g.us","person":"999999@lid"}`}},
		{{Kind: "mention-token", ID: "not-a-token", Value: `{"session":"` + scope + `","chat":"12345@g.us","person":"999999@lid"}`}},
		{{Kind: "mention-token", ID: "aabbccdd", Value: `{"session":"` + scope + `","chat":"status@broadcast","person":"999999@lid"}`}},
		{{Kind: "sync-group", ID: "other-account:12345@g.us", Value: `{"name":"Foreign","members":[],"fetchedAt":1}`}},
		{{Kind: "sync-group", ID: scope + ":12345@g.us", Value: `{"name":"Incomplete","fetchedAt":1}`}},
		{{Kind: "sync-group", ID: scope + ":12345@g.us", Value: `{"name":"Incomplete","members":null,"fetchedAt":1}`}},
		{{Kind: "sync-group", ID: scope + ":12345@g.us", Value: `{"name":"Invalid member","members":[{"id":"wrong"}],"fetchedAt":1}`}},
		{{Kind: "contact", ID: "999999@lid", Value: `{"bot":"missing"}`}},
		{{Kind: "contact", ID: "999999@lid", Value: `{"bot":"77"}`}, {Kind: "contact", ID: "12025550123@c.us", Value: `{"bot":"88"}`}},
		{{Kind: "contact", ID: "999999@lid", Value: `{"bot":"77"}`}, {Kind: "contact", ID: "12025550124@c.us", Value: `{"bot":"77"}`}},
	} {
		if _, err := ProjectDirectory("original-account", values, []Alias{{Alias: "999999@lid", Canonical: "12025550123@c.us"}}, []identity.Bot{{ID: "77"}, {ID: "88"}}); err == nil {
			t.Fatal("unsafe scoped directory projection accepted")
		}
	}
}
