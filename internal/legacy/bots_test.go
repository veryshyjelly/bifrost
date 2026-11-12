package legacy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/bots"
	"github.com/veryshyjelly/bifrost/internal/secrets"
)

func TestProjectBotsPreservesCiphertextAndSeparatesStartLogoutAndLocalEligibility(t *testing.T) {
	components, sealed := fixtureComponents(t, "original-account")
	c, err := ParseConfiguration(components["bridge.env"], components["telegram-api.env"])
	if err != nil {
		t.Fatal(err)
	}
	bot, _ := json.Marshal(map[string]any{"token": sealed, "username": "registered_name", "ready": true})
	values := []KV{
		{Kind: "bot", ID: "77", Value: string(bot)},
		{Kind: "migrated", ID: "77", Value: "true"},
		{Kind: "telegram-identity", ID: "77", Value: `{"id":77,"is_bot":true,"first_name":"Original","username":"stale_cached_name"}`},
		{Kind: "migrated", ID: "master", Value: "true"},
	}
	result, err := ProjectBots(c, values)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatal("missing master or member")
	}
	member, master := result[0], result[1]
	if member.Credential.BotID != "77" || member.Credential.Ciphertext != sealed || member.Credential.Revision != bots.Digest("77:preserve-original-token") || !member.Started || !member.HostedLogout || member.Credential.Identity.Username != "registered_name" || !member.Credential.CreatedNotice {
		t.Fatal("member state changed or old creation notice would repeat")
	}
	cipher, _ := secrets.New(c.EncryptionKey())
	plain, err := cipher.Open(master.Credential.Ciphertext)
	if err != nil || plain != c.Value("TELEGRAM_MASTER_TOKEN") || master.Credential.BotID != "master" || !master.HostedLogout || master.Started || master.Credential.Identity.ID != 42 || master.Credential.Identity.Username != "" {
		t.Fatal("master credential or eligibility evidence changed")
	}
	// A ready bot without logout evidence still requires hosted logout. A
	// cached native identity by itself does not establish any lifecycle state.
	withoutLogout, err := ProjectBots(c, []KV{values[0], values[2]})
	if err != nil || !withoutLogout[0].Started || withoutLogout[0].HostedLogout {
		t.Fatal("Start or cached identity promoted to logout proof")
	}
}

func TestProjectBotsRejectsOrphanConflictingAndInvalidSourceEvidence(t *testing.T) {
	components, sealed := fixtureComponents(t, "original-account")
	c, err := ParseConfiguration(components["bridge.env"], components["telegram-api.env"])
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"token": sealed, "username": "fixture_bot", "ready": false})
	base := KV{Kind: "bot", ID: "77", Value: string(raw)}
	for _, values := range [][]KV{
		{{Kind: "migrated", ID: "999", Value: "true"}},
		{{Kind: "telegram-identity", ID: "999", Value: `{"id":999,"is_bot":true}`}},
		{base, {Kind: "telegram-identity", ID: "77", Value: `{"id":88,"is_bot":true}`}},
		{base, {Kind: "telegram-identity", ID: "77", Value: `{"id":77,"is_bot":false}`}},
		{base, {Kind: "migrated", ID: "77", Value: "false"}},
		{base, base},
		{{Kind: "bot", ID: "77", Value: strings.ReplaceAll(string(raw), sealed, "wrong-ciphertext")}},
	} {
		if _, err := ProjectBots(c, values); err == nil {
			t.Fatal("invalid imported credential evidence accepted")
		}
	}
}
