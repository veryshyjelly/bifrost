package legacy

import (
	"reflect"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/identity"
)

func TestProjectChangesPreservesEmptyEditsMentionsHashesAndExplicitTombstones(t *testing.T) {
	d, _ := identity.New([]identity.Alias{{ID: "999999@lid", Canonical: "12025550123@c.us"}}, nil, nil, nil)
	values := []KV{
		{Kind: "latest-edit", ID: "999999:2@lid:message:device", Value: `{"body":"","mentionedIds":[]}`},
		{Kind: "latest-edit", ID: "12345@g.us:edited", Value: `{"body":"Edited 👋","mentionedIds":[{"_serialized":"888888@lid"}]}`},
		{Kind: "latest-edit", ID: "12345@g.us:unspecified", Value: `{"body":"Retain event mentions"}`},
		{Kind: "outbound-edit", ID: "999999@lid:echo", Value: `"` + strings.Repeat("a", 64) + `"`},
		{Kind: "revoked", ID: "999999@lid:gone", Value: `true`},
		{Kind: "revoked", ID: "12025550123@c.us:gone", Value: `true`},
		{Kind: "revoked", ID: "12345@g.us:not-confirmed", Value: `false`},
	}
	got, err := ProjectChanges(values, d)
	if err != nil || len(got.Edits) != 3 || len(got.Outbound) != 1 || len(got.Revoked) != 1 {
		t.Fatal("saved message state not retained", err)
	}
	if got.Edits[2].ChatID != "12025550123@c.us" || got.Edits[2].MessageID != "message:device" || got.Edits[2].Body != "" || got.Edits[2].MentionedIDs == nil || got.Edits[1].MentionedIDs != nil || !reflect.DeepEqual(got.Edits[0].MentionedIDs, []string{"888888@lid"}) {
		t.Fatal("empty edit, mention presence, alias or message colon changed")
	}
	if got.Outbound[0].ChatID != "12025550123@c.us" || got.Outbound[0].BodyHash != strings.Repeat("a", 64) || got.Revoked[0].MessageID != "gone" {
		t.Fatal("outbound hash or confirmed tombstone changed")
	}
}

func TestProjectChangesRejectsAmbiguousAliasVersionsAndMalformedState(t *testing.T) {
	d, _ := identity.New([]identity.Alias{{ID: "999999@lid", Canonical: "12025550123@c.us"}}, nil, nil, nil)
	for _, values := range [][]KV{
		{{Kind: "latest-edit", ID: "999999@lid:m", Value: `{"body":"old"}`}, {Kind: "latest-edit", ID: "12025550123@c.us:m", Value: `{"body":"new"}`}},
		{{Kind: "latest-edit", ID: "999999@lid:m", Value: `{"body":"same"}`}, {Kind: "latest-edit", ID: "12025550123@c.us:m", Value: `{"body":"same","mentionedIds":[]}`}},
		{{Kind: "outbound-edit", ID: "999999@lid:m", Value: `"` + strings.Repeat("a", 64) + `"`}, {Kind: "outbound-edit", ID: "12025550123@c.us:m", Value: `"` + strings.Repeat("b", 64) + `"`}},
		{{Kind: "latest-edit", ID: "12345@g.us:m", Value: `{"body":null}`}},
		{{Kind: "latest-edit", ID: "12345@g.us:m", Value: `{"body":"a","body":"b"}`}},
		{{Kind: "latest-edit", ID: "12345@g.us:m", Value: `{"body":"a","mentionedIds":null}`}},
		{{Kind: "latest-edit", ID: "12345@g.us:m", Value: `{"body":"a","mentionedIds":{}}`}},
		{{Kind: "outbound-edit", ID: "12345@g.us:m", Value: `"not-a-body-hash"`}},
		{{Kind: "outbound-edit", ID: "12345@g.us:m", Value: `"` + strings.Repeat("A", 64) + `"`}},
		{{Kind: "revoked", ID: "12345@g.us:m", Value: `null`}},
		{{Kind: "revoked", ID: "12345@g.us:m", Value: `1`}},
		{{Kind: "revoked", ID: "malformed", Value: `true`}},
	} {
		if _, err := ProjectChanges(values, d); err == nil {
			t.Fatal("ambiguous or malformed saved state accepted")
		}
	}
	if _, err := ProjectChanges(nil, nil); err == nil {
		t.Fatal("missing identity directory accepted")
	}
}
