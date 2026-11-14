// Package identity keeps verified WhatsApp aliases separate from display names.
// Device suffixes and the two phone JID domains denote the same identifier;
// LIDs become phone identities only through explicit provider evidence.
package identity

import (
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

var ErrConflict error = conflict{}

type conflict struct{}

func (conflict) Error() string { return "conflicting contact identity; manual review required" }

// A conflict is a definite local refusal, never an uncertain external send.
func (conflict) DeliveryOutcome() (bool, bool, time.Duration) { return false, false, 0 }

var person = regexp.MustCompile(`^([0-9]+)(?::[0-9]+)?@(c\.us|s\.whatsapp\.net|lid)$`)
var phone = regexp.MustCompile(`^[0-9]{5,15}$`)

func Normalize(id string) string {
	m := person.FindStringSubmatch(id)
	if m == nil {
		return id
	}
	domain := m[2]
	if domain == "s.whatsapp.net" {
		domain = "c.us"
	}
	return m[1] + "@" + domain
}
func IsPerson(id string) bool { return person.MatchString(id) }
func PhoneID(number string) (string, bool) {
	if !phone.MatchString(number) {
		return "", false
	}
	return number + "@c.us", true
}
func Local(id string) string { return strings.Split(Normalize(id), "@")[0] }

// CleanName matches the baseline mention label sanitizer and its 48-codepoint cap.
func CleanName(name string) string {
	runes := []rune(strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune("{}|", r) {
			return ' '
		}
		return r
	}, name))
	runes = []rune(strings.TrimFunc(string(runes), func(r rune) bool { return r == 0xfeff || (r != 0x85 && unicode.IsSpace(r)) }))
	if len(runes) > 48 {
		runes = runes[:48]
	}
	return string(runes)
}
func display(name string) string {
	name = CleanName(name)
	if name == "" || IsPerson(name) {
		return ""
	}
	if strings.TrimFunc(name, func(r rune) bool {
		return r == '+' || r == '.' || r == '-' || r == '(' || r == ')' || unicode.IsSpace(r) || r >= '0' && r <= '9'
	}) == "" {
		return ""
	}
	return name
}

type Alias struct{ ID, Canonical string }
type Profile struct {
	ID, Name string
	Synced   bool
}
type Bot struct {
	ID, Username string
	Started      bool
}
type Binding struct{ PersonID, BotID string }
type Member struct {
	ID, Name              string
	IsAdmin, IsSuperAdmin bool
}
type Person struct {
	ID, Name   string
	Bot        Bot
	Registered bool
}

// Directory is an immutable snapshot. It retains source profiles so a device
// placeholder cannot overwrite a readable cached name or hide a bound bot.
type Directory struct {
	aliases       map[string]string
	profiles      []Profile
	bots          map[string]Bot
	bindings      map[string]Bot
	boundProfiles map[string]string
}

func New(aliases []Alias, profiles []Profile, bots []Bot, bindings []Binding) (*Directory, error) {
	d := &Directory{aliases: map[string]string{}, profiles: append([]Profile(nil), profiles...), bots: map[string]Bot{}, bindings: map[string]Bot{}, boundProfiles: map[string]string{}}
	for _, a := range aliases {
		id, canonical := Normalize(a.ID), Normalize(a.Canonical)
		verified, valid := PhoneID(Local(canonical))
		if !IsPerson(id) || !strings.HasSuffix(id, "@lid") || !valid || verified != canonical {
			return nil, ErrConflict
		}
		if old, ok := d.aliases[id]; ok && old != canonical {
			return nil, ErrConflict
		}
		d.aliases[id] = canonical
	}
	for _, b := range bots {
		d.bots[b.ID] = b
	}
	for _, b := range bindings {
		bot, ok := d.bots[b.BotID]
		if !ok {
			continue
		}
		id := d.Canonical(b.PersonID)
		if old, ok := d.bindings[id]; ok && old.ID != bot.ID {
			return nil, ErrConflict
		}
		d.bindings[id] = bot
		d.boundProfiles[id] = b.PersonID
	}
	return d, nil
}
func (d *Directory) Canonical(id string) string {
	id = Normalize(id)
	if canonical, ok := d.aliases[id]; ok {
		return canonical
	}
	return id
}
func (d *Directory) Aliases(id string) []string {
	id = d.Canonical(id)
	out := []string{id}
	for alias, canonical := range d.aliases {
		if canonical == id {
			out = append(out, alias)
		}
	}
	sort.Strings(out)
	return out
}
func (d *Directory) Person(id string, members []Member) Person {
	id = d.Canonical(id)
	p := Person{ID: id}
	p.Bot, p.Registered = d.bindings[id]
	// The old lookup prefers a bound contact, then the exact canonical profile,
	// then another readable equivalent profile. Placeholder names never win.
	for _, preferred := range []string{d.boundProfiles[id], id} {
		for _, c := range d.profiles {
			if preferred != "" && c.ID == preferred {
				if name := display(c.Name); name != "" {
					p.Name = name
					return p
				}
			}
		}
	}
	for _, c := range d.profiles {
		if d.Canonical(c.ID) == id {
			if name := display(c.Name); name != "" {
				p.Name = name
				return p
			}
		}
	}
	for _, m := range members {
		if d.Canonical(m.ID) == id {
			if name := display(m.Name); name != "" {
				p.Name = name
				return p
			}
		}
	}
	p.Name = Local(id)
	return p
}
func (d *Directory) ForBot(bot string) (string, bool) {
	for id, b := range d.bindings {
		if b.ID == bot {
			return id, true
		}
	}
	return "", false
}
func (d *Directory) ForUsername(username string) (string, bool) {
	var found string
	for id, b := range d.bindings {
		if b.Username != "" && strings.EqualFold(b.Username, username) {
			if found != "" && found != id {
				return "", false
			}
			found = id
		}
	}
	return found, found != ""
}
