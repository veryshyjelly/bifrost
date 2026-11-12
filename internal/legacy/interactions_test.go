package legacy

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/identity"
)

func TestLegacyInteractionStatePreservesExactChoicesAndFrozenResolution(t *testing.T) {
	d, _ := identity.New([]identity.Alias{{ID: "999999@lid", Canonical: "12025550123@c.us"}}, nil, nil, nil)
	values := []KV{
		{Kind: "poll-selection", ID: "999999@lid:poll:id", Value: `[" B ","A"," B "]`},
		{Kind: "poll-selection", ID: "12025550123@c.us:poll:id", Value: `[" B ","A"," B "]`},
		{Kind: "poll-selection", ID: "12345@g.us:cleared", Value: `[]`},
		{Kind: "custom-reaction-emoji", ID: "12345", Value: `{"emoji":"👩🏽‍💻"}`},
		{Kind: "outgoing-reaction", ID: strings.Repeat("a", 64), Value: `{"emoji":""}`},
		{Kind: "outgoing-reaction", ID: strings.Repeat("b", 64), Value: `{"unsupported":true}`},
		{Kind: "outgoing-reaction", ID: strings.Repeat("c", 64), Value: `{"emoji":"👍"}`},
	}
	state, err := ProjectInteractions(values, d)
	if err != nil || len(state.Polls) != 2 || len(state.Custom) != 1 || len(state.Choices) != 3 {
		t.Fatal("interaction state lost", err)
	}
	if state.Polls[0].ChatID != "12025550123@c.us" || !reflect.DeepEqual(state.Polls[0].Options, []string{" B ", "A", " B "}) || state.Polls[1].Options == nil || state.Custom[0].Emoji != "👩🏽‍💻" || !state.Choices[0].Choice.Supported || state.Choices[0].Choice.Emoji != "" || state.Choices[1].Choice.Supported || !state.Choices[2].Choice.Supported {
		t.Fatal("selection order, option text, empty reaction or unsupported selection changed")
	}
}

func TestLegacyInteractionResolutionKeysMatchJSONStringify(t *testing.T) {
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

func TestLegacyInteractionsRejectMalformedCachesAndUnorderedAliasSelections(t *testing.T) {
	d, _ := identity.New([]identity.Alias{{ID: "999999@lid", Canonical: "12025550123@c.us"}}, nil, nil, nil)
	for _, values := range [][]KV{
		{{Kind: "poll-selection", ID: "999999@lid:m", Value: `["A"]`}, {Kind: "poll-selection", ID: "12025550123@c.us:m", Value: `["B"]`}},
		{{Kind: "poll-selection", ID: "12345@g.us:m", Value: `null`}},
		{{Kind: "poll-selection", ID: "12345@g.us:m", Value: `[1]`}},
		{{Kind: "poll-selection", ID: "12345@g.us:m", Value: `[null]`}},
		{{Kind: "custom-reaction-emoji", ID: "123", Value: `{"emoji":"two 👋 emoji 👍"}`}},
		{{Kind: "outgoing-reaction", ID: "wrong-key", Value: `{"emoji":"👍"}`}},
		{{Kind: "outgoing-reaction", ID: strings.Repeat("a", 64), Value: `{"emoji":"👍","unsupported":true}`}},
		{{Kind: "outgoing-reaction", ID: strings.Repeat("a", 64), Value: `{"unsupported":false}`}},
		{{Kind: "outgoing-reaction", ID: strings.Repeat("a", 64), Value: `{"emoji":null}`}},
		{{Kind: "outgoing-reaction", ID: strings.Repeat("a", 64), Value: `{"emoji":"not-emoji"}`}},
	} {
		if _, err := ProjectInteractions(values, d); err == nil {
			t.Fatal("invalid or ambiguous interaction cache accepted")
		}
	}
}
