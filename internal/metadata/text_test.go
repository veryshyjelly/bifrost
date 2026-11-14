package metadata

import "testing"

func TestClipPreservesBaselineUTF16BudgetAndAllowedWhitespace(t *testing.T) {
	for _, tc := range []struct {
		in   string
		n    int
		want string
	}{
		{"a😀b", 2, "a"}, {"a😀b", 3, "a😀"}, {"\x00a\x08b\x0bc\x1fd\t\n\r", 20, "abcd\t\n\r"}, {"title", 0, ""},
	} {
		if got := Clip(tc.in, tc.n); got != tc.want {
			t.Fatalf("Clip(%q,%d)=%q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}
