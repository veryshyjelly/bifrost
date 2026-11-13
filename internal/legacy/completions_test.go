package legacy

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/identity"
)

func TestHistoryDestinationKeyMatchesOriginalMixedJSONTuple(t *testing.T) {
	account := "account<&\u2028"
	raw := "[\"account<&\u2028\",\"12345@g.us\",\"-100\",44,\"*\"]"
	sum := sha256.Sum256([]byte(raw))
	want := hex.EncodeToString(sum[:]) + ":message:part"
	if got := HistoryDestinationKey(account, "12345@g.us", "message:part", "master", -100, 44); got != want {
		t.Fatal("legacy mixed tuple encoding changed")
	}
	if got := HistoryDestinationKey(account, "12345@g.us", "message:part", "77", -100, 44); got != want {
		t.Fatal("group scope became bot-specific")
	}
	if HistoryDestinationKey(account, "12345@g.us", "message:part", "77", 123, 0) == HistoryDestinationKey(account, "12345@g.us", "message:part", "master", 123, 0) {
		t.Fatal("private bot scopes collapsed")
	}
}

func TestSplitMessageKeyPreservesDeviceAndMessageIDColons(t *testing.T) {
	for _, tc := range []struct{ key, chat, id string }{
		{"12025550123:7@c.us:first:part", "12025550123:7@c.us", "first:part"},
		{"999999:2@lid:first:part", "999999:2@lid", "first:part"},
		{"12025550123@s.whatsapp.net:first", "12025550123@s.whatsapp.net", "first"},
		{"12345@g.us:message@c.us:part", "12345@g.us", "message@c.us:part"},
		{"status@broadcast:message@lid:part", "status@broadcast", "message@lid:part"},
	} {
		chat, id, err := SplitMessageKey(tc.key)
		if err != nil || chat != tc.chat || id != tc.id {
			t.Fatal("compound source key changed", tc.key)
		}
	}
	for _, key := range []string{"missing-jid:message", "12345@g.us:", "999@wrong:message", "status@broadcast:"} {
		if _, _, err := SplitMessageKey(key); err == nil {
			t.Fatal("invalid compound source key accepted")
		}
	}
}

func TestProjectCompletionsUsesOnlyExplicitTrueMarkers(t *testing.T) {
	d, _ := identity.New([]identity.Alias{{ID: "999999@lid", Canonical: "12025550123@c.us"}}, nil, nil, nil)
	values := []KV{{Kind: "wa-complete", ID: "999999:2@lid:first:part", Value: "true"}, {Kind: "wa-complete", ID: "12025550123@c.us:first:part", Value: "true"}, {Kind: "wa-complete", ID: "12345@g.us:unfinished", Value: "false"}}
	result, err := ProjectCompletions(values, d)
	if err != nil || len(result) != 1 || result[0].ChatID != "12025550123@c.us" || result[0].MessageID != "first:part" || result[0].SourceKey != "12025550123@c.us:first:part" {
		t.Fatal("explicit completion identity or source proof changed")
	}
	for _, value := range []string{"null", " null ", "1", `"true"`, `{"done":true}`} {
		if _, err := ProjectCompletions([]KV{{Kind: "wa-complete", ID: "12345@g.us:message", Value: value}}, d); err == nil {
			t.Fatal("non-boolean completion evidence accepted")
		}
	}
}
