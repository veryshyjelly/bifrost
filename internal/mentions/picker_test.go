package mentions

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func TestPickerPresentationMatchesLegacyNodeGolden(t *testing.T) {
	var fixture struct {
		Locale string
		Cases  []struct {
			Input struct {
				Name     string
				Members  []struct{ ID, Name string }
				Aliases  map[string]string
				Profiles []identity.Profile
				Bots     []identity.Bot
				Bindings []identity.Binding
				Cached   bool
				Search   string
				Page     int
			}
			Expected struct {
				Text     string
				Entities []models.MessageEntity
				Markup   models.InlineKeyboardMarkup
			}
		}
	}
	data, err := os.ReadFile("testdata/picker.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Locale != "en-US" {
		t.Fatal("unexpected oracle collation locale", fixture.Locale)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Input.Name, func(t *testing.T) {
			f := newFixture(t)
			aliases := []identity.Alias{}
			for id, c := range tc.Input.Aliases {
				aliases = append(aliases, identity.Alias{ID: id, Canonical: c})
			}
			f.directory, err = identity.New(aliases, tc.Input.Profiles, tc.Input.Bots, tc.Input.Bindings)
			if err != nil {
				t.Fatal(err)
			}
			f.members = []sdk.GroupParticipant{}
			rosterMembers := []identity.Member{}
			for _, m := range tc.Input.Members {
				f.members = append(f.members, sdk.GroupParticipant{ID: m.ID, Name: m.Name})
				rosterMembers = append(rosterMembers, identity.Member{ID: m.ID, Name: m.Name})
			}
			if tc.Input.Cached {
				f.rosters[group] = Roster{ChatID: group, Name: "Old", FetchedAt: f.s.now().Add(-6 * time.Minute).UnixMilli(), Members: rosterMembers}
				f.groupErr = ErrRoster
			}
			p := &Picker{mentions: f.s, c: PickerOptions{OwnerID: 123, ControlTTL: 600 * time.Second, CommandTTL: 60 * time.Second, Now: f.s.now}}
			c := control.Context{Known: true, Target: control.Target{BotID: "master", ChatID: -200}, Route: conversation.Route{ChatID: group, TelegramChatID: -200, Mode: conversation.Group}}
			plan := control.Plan{Key: "fixture", Target: c.Target}
			if err := p.page(t.Context(), f.job, &plan, c, tc.Input.Search, tc.Input.Page); err != nil {
				t.Fatal(err)
			}
			if len(plan.Packets) != 1 {
				t.Fatal("unexpected golden packet split", len(plan.Packets))
			}
			got := plan.Packets[0]
			for i, a := range plan.Actions {
				for ri, row := range got.Markup.InlineKeyboard {
					for bi, b := range row {
						if b.CallbackData == "mention:"+a.ID {
							got.Markup.InlineKeyboard[ri][bi].CallbackData = fmt.Sprintf("mention:fixture-action-%d", i+1)
						}
					}
				}
			}
			if got.Text != tc.Expected.Text || !reflect.DeepEqual(got.Entities, tc.Expected.Entities) || !reflect.DeepEqual(*got.Markup, tc.Expected.Markup) {
				t.Fatalf("legacy picker changed\ngot text: %s\nwant text: %s\ngot entities: %+v\nwant entities: %+v\ngot keyboard: %+v\nwant keyboard: %+v", got.Text, tc.Expected.Text, got.Entities, tc.Expected.Entities, *got.Markup, tc.Expected.Markup)
			}
		})
	}
}

func TestMentionRefusalDoesNotConvertAnUncertainProviderFailureIntoANotice(t *testing.T) {
	p := &Picker{c: PickerOptions{OwnerID: 123}}
	m := &models.Message{From: &models.User{ID: 123}}
	handled, err := p.Refusal(t.Context(), delivery.Job{}, nil, "master", m, &providers.Error{Service: "openwa", Status: 503, Uncertain: true})
	if handled || err != nil {
		t.Fatal("provider uncertainty was replaced by a mention notice", handled, err)
	}
}
