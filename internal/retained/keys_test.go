package retained

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestHistoryDestinationKeyMatchesOriginalMixedJSONTuple(t *testing.T) {
	account := "account<&\u2028"
	raw := "[\"account<&\u2028\",\"12345@g.us\",\"-100\",44,\"*\"]"
	sum := sha256.Sum256([]byte(raw))
	want := hex.EncodeToString(sum[:]) + ":message:part"
	if got := HistoryDestinationKey(account, "12345@g.us", "message:part", "master", -100, 44); got != want {
		t.Fatal("retained mixed tuple encoding changed")
	}
	if got := HistoryDestinationKey(account, "12345@g.us", "message:part", "77", -100, 44); got != want {
		t.Fatal("group scope became bot-specific")
	}
	if HistoryDestinationKey(account, "12345@g.us", "message:part", "77", 123, 0) == HistoryDestinationKey(account, "12345@g.us", "message:part", "master", 123, 0) {
		t.Fatal("private bot scopes collapsed")
	}
}

func TestRetainedInteractionResolutionKeysMatchJSONStringify(t *testing.T) {
	for _, tc := range []struct{ account, bot, key, raw string }{
		{"fixture-account", "master", "tg:master:123", `["fixture-account","master","tg:master:123"]`},
		{"<>&\u2028\u2029", "77", "a\"\\\n\t\b\f\r\x01", "[\"<>&\u2028\u2029\",\"77\",\"a\\\"\\\\\\n\\t\\b\\f\\r\\u0001\"]"},
	} {
		sum := sha256.Sum256([]byte(tc.raw))
		if got := OutgoingReactionKey(tc.account, tc.bot, tc.key); got != hex.EncodeToString(sum[:]) {
			t.Fatal("original JSON tuple digest changed")
		}
	}
}
