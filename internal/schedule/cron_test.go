package schedule

import (
	"testing"
	"time"
)

func TestLegacyCronGrammarAndTimezone(t *testing.T) {
	for _, tc := range []struct {
		expr, zone, at string
		want           bool
	}{
		{"0 * * * *", "Asia/Kolkata", "2026-11-11T00:30:45Z", true},
		{"0 * * * *", "UTC", "2026-11-11T00:30:00Z", false},
		{"*/15 * * * *", "UTC", "2026-11-11T00:45:59Z", true},
		{"0 0 * * 7", "UTC", "2026-11-15T00:00:00Z", true},
		{"0 0 * * 0", "UTC", "2026-11-15T00:00:00Z", true},
		{"0 0 1 * 1", "UTC", "2026-11-16T00:00:00Z", true},
		{"0 0 */1 * 1", "UTC", "2026-11-17T00:00:00Z", true},
		{"0 0 * * 1", "UTC", "2026-11-17T00:00:00Z", false},
		{"0 0 31 2 *", "UTC", "2026-02-28T00:00:00Z", false},
		{"5/15 0-2 * 1,11 1-7/2", "UTC", "2026-11-15T01:35:00Z", true},
		{"5/9999999999999999999999999 * * * *", "UTC", "2026-11-15T01:05:00Z", true},
		{"30 1 * * *", "America/New_York", "2026-11-01T05:30:00Z", true},
		{"30 1 * * *", "America/New_York", "2026-11-01T06:30:00Z", true},
		{"off", "not-a-timezone", "2026-11-11T00:00:00Z", false},
	} {
		t.Run(tc.expr+tc.at, func(t *testing.T) {
			c, err := New(tc.expr, tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			at, err := time.Parse(time.RFC3339, tc.at)
			if err != nil {
				t.Fatal(err)
			}
			if c.Matches(at) != tc.want {
				t.Fatal(tc)
			}
		})
	}
	for _, expr := range []string{"* * * *", "* * * * * *", "@hourly", "? * * * *", "0 24 * * *", "60 * * * *", "*/0 * * * *", "0 0 * * MON", "0 0 0 * *", "0 0 * 13 *", "0 0 * * 8", "1,,2 * * * *", "10-2 * * * *", " off "} {
		if _, err := New(expr, "UTC"); err == nil {
			t.Fatal("invalid legacy cron accepted", expr)
		}
	}
	if _, err := New("* * * * *", "Local"); err == nil {
		t.Fatal("host-local timezone accepted")
	}
}
