package control

import (
	"github.com/go-telegram/bot/models"
	"reflect"
	"testing"
)

func TestCommandParsingPreservesLegacyCasingCaptionAndUnicodeWhitespace(t *testing.T) {
	for _, tc := range []struct {
		text string
		want ParsedCommand
		ok   bool
	}{
		{" \ufeff/MENTION@Alice_BOT\u2003Person\n  One ", ParsedCommand{Name: "mention", Recipient: "alice_bot", Args: []string{"Person", "One"}}, true},
		{"/mention", ParsedCommand{Name: "mention", Args: []string{}}, true},
		{"/mention-bad", ParsedCommand{}, false},
		{"/mention\u0085name", ParsedCommand{}, false},
		{"/mention@bifroſt_bot", ParsedCommand{}, false},
	} {
		got, ok := ParseCommand(&models.Message{Text: tc.text})
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Fatal(tc.text, got, ok)
		}
	}
	got, ok := ParseCommand(&models.Message{Caption: "/mention Alice"})
	if !ok || got.Name != "mention" || len(got.Args) != 1 {
		t.Fatal(got, ok)
	}
}
