package metadata

import (
	"fmt"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/presentation"
)

func TestMetadataReportPreservesWarningAndButtonLimits(t *testing.T) {
	input := ReportInput{Report: Report{Groups: 2, Contacts: 3, Changed: 4, SkippedPhotos: 5}, Warnings: []string{"one", "one", "two", "three", "four", "five"}, Destinations: []control.Target{{BotID: "master", ChatID: 123}}}
	for i := 0; i < 10; i++ {
		input.Missing = append(input.Missing, BotAdvice{Name: strings.Repeat("😀", 30), Username: fmt.Sprintf("bot_%d", i)})
	}
	for i := 0; i < 5; i++ {
		input.Permissions = append(input.Permissions, PermissionAdvice{Name: strings.Repeat("😀", 30), URL: "https://t.me/fixture_bot?startgroup=sync_fixture"})
	}
	packets, buttons, err := ReportPackets(input)
	if err != nil || len(packets) != 1 || buttons != 11 {
		t.Fatal(len(packets), buttons, err)
	}
	if !strings.Contains(packets[0].Text, "6 warnings; 10 contact bots need adding; 5 pictures unavailable or hidden.") || strings.Count(packets[0].Text, "\none") != 1 || strings.Contains(packets[0].Text, "\nfive") {
		t.Fatal(packets[0].Text)
	}
	rows := packets[0].Markup.InlineKeyboard
	if rows[0][0].Text != "Add "+strings.Repeat("😀", 20) || rows[8][0].Text != "Allow info edits · "+strings.Repeat("😀", 15) {
		t.Fatal(rows)
	}
}

func TestMetadataReportSplitsNativeTextAndDeduplicatesDestinations(t *testing.T) {
	target := control.Target{BotID: "master", ChatID: 123}
	input := ReportInput{Warnings: []string{strings.Repeat("😀", 3000)}, Destinations: []control.Target{target, target}}
	packets, _, err := ReportPackets(input)
	if err != nil || len(packets) != 2 {
		t.Fatal(len(packets), err)
	}
	for _, p := range packets {
		if presentation.Length(p.Text) > 3800 || *p.Target != target {
			t.Fatal("native packet/target changed", p)
		}
	}
}
