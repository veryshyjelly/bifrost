package history

import (
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"testing"
)

func TestHistoryCountsAndQuoteDependencyOrdering(t *testing.T) {
	if n, ok := Count(nil, 20); !ok || n != 20 {
		t.Fatal(n, ok)
	}
	for _, arg := range []string{"0", "2001", "-1", "+20", "1.5", "1e2", "", "999999999999999999999999"} {
		if _, ok := Count([]string{arg}, 20); ok {
			t.Fatal(arg)
		}
	}
	if n, ok := Count([]string{"02000"}, 20); !ok || n != 2000 {
		t.Fatal(n, ok)
	}
	rows := []sdk.ChatHistoryMessage{{ID: "child", Timestamp: 1, QuotedMessage: &sdk.QuotedMessage{ID: "parent"}}, {ID: "parent", Timestamp: 2}, {ID: "cycle-a", Timestamp: 3, QuotedMessage: &sdk.QuotedMessage{ID: "cycle-b"}}, {ID: "cycle-b", Timestamp: 4, QuotedMessage: &sdk.QuotedMessage{ID: "cycle-a"}}}
	got := Ordered(rows)
	if len(got) != 4 || got[0].ID != "parent" || got[1].ID != "child" {
		t.Fatal(got)
	}
	seen := map[string]bool{}
	for _, m := range got {
		if seen[m.ID] {
			t.Fatal("duplicate", m.ID)
		}
		seen[m.ID] = true
	}
}
