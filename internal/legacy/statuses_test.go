package legacy

import (
	"encoding/json"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/identity"
)

func TestProjectStatusesKeepsFeedMuteRevisionSkipsAndOriginalOperations(t *testing.T) {
	components, _ := fixtureComponents(t, "account")
	c, err := ParseConfiguration(components["bridge.env"], components["telegram-api.env"])
	if err != nil {
		t.Fatal(err)
	}
	d, _ := identity.New([]identity.Alias{{ID: "999999@lid", Canonical: "12025550123@c.us"}}, nil, nil, nil)
	scope := StatusScope("account")
	id := stringTupleKey(scope, "999999@lid")
	mute, _ := json.Marshal(map[string]any{"id": id, "session": scope, "sender": "999999@lid", "name": "Original name", "active": true, "revision": 7, "updatedAt": 1700000000000})
	feed, _ := json.Marshal(map[string]any{"chatId": "status@broadcast", "tgChat": c.Value("TELEGRAM_MANAGEMENT_CHAT_ID"), "thread": 60, "mode": "status"})
	values := []KV{{Kind: "status-feed", ID: stringTupleKey("account", c.Value("TELEGRAM_MANAGEMENT_CHAT_ID")), Value: string(feed)}, {Kind: "status-mute", ID: id, Value: string(mute)}, {Kind: "status-mute-counter", ID: scope, Value: "9"}, {Kind: "status-skipped", ID: StatusSkipKey("account", "never-enqueued"), Value: `{"sender":"999999@lid","at":1700000000000}`}, {Kind: "status-mute-op", ID: stringTupleKey(scope, c.Value("TELEGRAM_OWNER_ID"), "original-job"), Value: string(mute)}}
	jobs := []Job{{Type: "tg", Dedup: "original-job"}}
	state, err := ProjectStatuses("account", c, values, jobs, d)
	if err != nil || state.Feed == nil || state.Feed.Feed.ThreadID != 60 || len(state.Mutes) != 1 || state.Mutes[0].Author != "12025550123@c.us" || state.Revision != 9 || len(state.Skipped) != 1 || state.Operations[0].JobKey != "original-job" {
		t.Fatal("saved Status state lost", err)
	}
	for _, changed := range []struct {
		index int
		value string
	}{{2, "6"}, {2, "null"}, {3, `{"sender":"12345@g.us","at":1}`}, {1, `{"session":"foreign","sender":"999999@lid"}`}} {
		bad := append([]KV(nil), values...)
		bad[changed.index].Value = changed.value
		if _, err := ProjectStatuses("account", c, bad, jobs, d); err == nil {
			t.Fatal("invalid scoped/revision state accepted")
		}
	}
	if _, err := ProjectStatuses("account", c, values, nil, d); err == nil {
		t.Fatal("mute operation invented an original job")
	}
}
