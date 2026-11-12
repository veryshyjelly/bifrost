package legacy

import (
	"encoding/json"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/identity"
)

func TestProjectSourcesPreservesSenderIdentityAndOnlyRetainedContext(t *testing.T) {
	d, _ := identity.New([]identity.Alias{{ID: "999999@lid", Canonical: "12025550123@c.us"}}, nil, nil, nil)
	values := []KV{
		{Kind: "wamessage", ID: "999999:2@lid:private:id", Value: `{"sender":"999999@lid","name":"Older alias","fromMe":true,"linkSender":"12025550999@c.us","historical":true,"timestamp":1700000000}`},
		{Kind: "wamessage", ID: "12025550123@c.us:private:id", Value: `{"sender":"12025550123@c.us","name":"Canonical cache","fromMe":true,"linkSender":"12025550999@c.us","mentionedIds":[]}`},
		{Kind: "wamessage", ID: "12345@g.us:group:id", Value: `{"sender":"888888:3@lid","name":"Original participant","fromMe":true,"linkSender":"","mentionedIds":[{"$1":"999999@lid"}]}`},
	}
	sources, err := ProjectSources(values, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || sources[0].MessageID != "private:id" || sources[0].Name != "Canonical cache" || sources[0].SenderID != "12025550123@c.us" || sources[0].MentionedIDs == nil || sources[1].SenderID != "888888@lid" || sources[1].LinkSenderID != "" || len(sources[1].MentionedIDs) != 1 {
		t.Fatal("source identity/context changed")
	}
	raw, _ := json.Marshal(sources[1])
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if _, exists := fields["Body"]; exists {
		t.Fatal("missing original content fabricated as a source body")
	}
}

func TestLegacyUnboundDisplayCachesDoNotBecomeBotOrPhoneBindings(t *testing.T) {
	values := []KV{{Kind: "contact", ID: "12345@g.us", Value: `{"name":"Legacy group display cache"}`}, {Kind: "contact", ID: "unknown", Value: `{"name":"Legacy unknown sender","number":"12025550999"}`}}
	projection, err := ProjectDirectory("original-account", values, nil, nil)
	if err != nil || len(projection.Profiles) != 2 || len(projection.Bindings) != 0 || len(projection.Aliases) != 0 {
		t.Fatal("unbound legacy display caches lost or became identity evidence")
	}
	values[0].Value = `{"name":"Group cache","bot":"77"}`
	if _, err := ProjectDirectory("original-account", values, nil, []identity.Bot{{ID: "77"}}); err == nil {
		t.Fatal("non-person display cache gained a contact bot binding")
	}
}
