package presentation

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
)

func TestLegacyRenderingAndSplittingGoldenFixtures(t *testing.T) {
	var cases []struct {
		Options  Options
		Body     string
		Limit    int
		Rendered Value
		Parts    []Value
	}
	b, err := os.ReadFile("testdata/legacy.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		got, err := Relay(c.Options, Plain(c.Body))
		if err != nil || !reflect.DeepEqual(got, c.Rendered) {
			t.Fatalf("case %d rendering: %+v != %+v (%v)", i, got, c.Rendered, err)
		}
		parts, err := Split(got, c.Limit)
		if err != nil || !reflect.DeepEqual(parts, c.Parts) {
			t.Fatalf("case %d splitting mismatch: %v", i, err)
		}
		var whole strings.Builder
		for _, p := range parts {
			whole.WriteString(p.Text)
			if Length(p.Text) > c.Limit {
				t.Fatal("split exceeded budget")
			}
		}
		if whole.String() != got.Text {
			t.Fatal("UTF-16 splitting lost text")
		}
	}
}

func TestPhoneLinksNeverTreatLIDOrUnverifiedJIDAsPhone(t *testing.T) {
	for _, id := range []string{"", "123456789012345@lid", "123@g.us", "111@c.us", "012025550123@c.us", "https://example.com@c.us"} {
		if WhatsAppURL(id) != "https://wa.me/" {
			t.Fatal(id)
		}
	}
	if WhatsAppURL("+12025550123@s.whatsapp.net") != "https://wa.me/12025550123" {
		t.Fatal("verified phone link changed")
	}
}

func TestAtomicDatesAndSurrogateBoundaries(t *testing.T) {
	v := Concat(Styled("😀😀", models.MessageEntityTypeBold), HistoryTime(1700000000, "History", time.Time{}))
	parts, err := Split(v, 25)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		for _, e := range part.Entities {
			if e.Type == models.MessageEntityTypeDateTime && e.Length != 20 {
				t.Fatal("native date split", e)
			}
		}
	}
	if _, err := Split(v, 4); err == nil {
		t.Fatal("overlong atomic span accepted")
	}
	if _, err := Slice(Plain("😀"), 0, 1); err == nil {
		t.Fatal("split surrogate pair")
	}
	if _, err := Split(Value{Text: "😀", Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBold, Offset: 1, Length: 1}}}, 3800); err == nil {
		t.Fatal("invalid entity boundary accepted")
	}
}

func TestHistoryClocksDaysAndUnknownTimes(t *testing.T) {
	for _, tc := range []struct{ zone, clock, key, label string }{{"Asia/Kolkata", "03:43", "2023-11-15", "15 November 2023"}, {"UTC", "22:13", "2023-11-14", "14 November 2023"}} {
		clock, err := HistoryClock(1700000000, tc.zone, time.Time{})
		if err != nil || clock.Text != tc.clock {
			t.Fatal(clock, err)
		}
		key, label, err := HistoryDay(1700000000, tc.zone, time.Time{})
		if err != nil || key != tc.key || label.Text != tc.label {
			t.Fatal(key, label, err)
		}
	}
	clock, _ := HistoryClock(1700000000000, "UTC", time.Time{})
	if clock.Text != "time unknown" {
		t.Fatal(clock)
	}
	if _, err := HistoryClock(1700000000, "invalid/location", time.Time{}); err == nil {
		t.Fatal("invented timezone")
	}
}
