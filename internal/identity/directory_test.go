package identity

import (
	"errors"
	"testing"
)

func TestNormalizationDoesNotGuessPrivacyPhones(t *testing.T) {
	for in, want := range map[string]string{"12025550123:7@s.whatsapp.net": "12025550123@c.us", "987654321:2@lid": "987654321@lid", "12025550123@lid": "12025550123@lid", "123@g.us": "123@g.us", "garbage": "garbage"} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q)=%q", in, got)
		}
	}
	d, err := New([]Alias{{"987654321:2@lid", "12025550123@s.whatsapp.net"}}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Canonical("987654321:9@lid") != "12025550123@c.us" || d.Canonical("555555@lid") != "555555@lid" {
		t.Fatal("alias was not explicit")
	}
	for _, bad := range []string{"+12025550123", "987654321@lid", "1234", "1234567890123456", "12345\n"} {
		if _, ok := PhoneID(bad); ok {
			t.Fatal("invalid phone accepted")
		}
	}
}
func TestReadableProfilesAndBoundBotsSurviveDevicePlaceholders(t *testing.T) {
	d, err := New([]Alias{{"987654321@lid", "12025550123@c.us"}}, []Profile{{ID: "12025550123:7@c.us", Name: "12025550123:7@c.us"}, {ID: "987654321:2@lid", Name: "987654321:2@lid"}, {ID: "12025550123@c.us", Name: "Alice"}}, []Bot{{ID: "42", Username: "alice_bot"}}, []Binding{{PersonID: "987654321@lid", BotID: "42"}})
	if err != nil {
		t.Fatal(err)
	}
	p := d.Person("987654321:8@lid", nil)
	if p.Name != "Alice" || !p.Registered || p.Bot.ID != "42" {
		t.Fatal(p)
	}
	if id, ok := d.ForUsername("ALICE_BOT"); !ok || id != p.ID {
		t.Fatal(id, ok)
	}
	if p := d.Person("12025550124@c.us", []Member{{ID: "12025550124:2@s.whatsapp.net", Name: "Roster Bob"}}); p.Name != "Roster Bob" {
		t.Fatal(p)
	}
}
func TestConflictingEvidenceCannotMergeTwoBotBindings(t *testing.T) {
	_, err := New([]Alias{{"987654321@lid", "12025550123@c.us"}}, nil, []Bot{{ID: "42"}, {ID: "43"}}, []Binding{{"987654321@lid", "42"}, {"12025550123@c.us", "43"}})
	if !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	_, err = New([]Alias{{"987654321@lid", "12025550123@c.us"}, {"987654321@lid", "12025550124@c.us"}}, nil, nil, nil)
	if !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestBoundProfileKeepsLegacyNamePrecedence(t *testing.T) {
	d, err := New([]Alias{{ID: "987654321@lid", Canonical: "12025550123@c.us"}}, []Profile{{ID: "12025550123@c.us", Name: "Canonical Alice", Synced: true}, {ID: "987654321@lid", Name: "Bound Alice"}}, []Bot{{ID: "42"}}, []Binding{{PersonID: "987654321@lid", BotID: "42"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Person("12025550123@c.us", nil); got.Name != "Bound Alice" {
		t.Fatal(got)
	}
}
