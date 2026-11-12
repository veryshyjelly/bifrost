package legacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/mentions"
)

type ImportedBinding struct{ PersonID, BotID, SourceProfile string }
type DirectoryProjection struct {
	Aliases  []identity.Alias
	Profiles []identity.Profile
	Bindings []ImportedBinding
	Tokens   map[string]mentions.Token
	Rosters  []mentions.Roster
}

var copiedToken = regexp.MustCompile(`^[a-f0-9]{8,64}$`)

// ProjectDirectory preserves explicit identity evidence, the selected source
// profile used for a bound name, and account-scoped copied mention tokens and
// roster snapshots. Display numbers and cached member canonical fields never
// establish new phone evidence.
func ProjectDirectory(account string, values []KV, aliases []Alias, bots []identity.Bot) (DirectoryProjection, error) {
	var result DirectoryProjection
	result.Tokens = map[string]mentions.Token{}
	if !validAccount(account) {
		return result, ErrInvalid
	}
	digest := sha256.Sum256([]byte(account))
	scope := hex.EncodeToString(digest[:])
	aliasMap := map[string]string{}
	for _, a := range aliases {
		id, canonical := identity.Normalize(a.Alias), identity.Normalize(a.Canonical)
		if previous, ok := aliasMap[id]; ok && previous != canonical {
			return result, ErrInvalid
		}
		aliasMap[id] = canonical
	}
	for id, canonical := range aliasMap {
		result.Aliases = append(result.Aliases, identity.Alias{ID: id, Canonical: canonical})
	}
	sort.Slice(result.Aliases, func(i, j int) bool { return result.Aliases[i].ID < result.Aliases[j].ID })
	directory, err := identity.New(result.Aliases, nil, bots, nil)
	if err != nil {
		return result, ErrInvalid
	}
	registered := map[string]bool{}
	for _, b := range bots {
		if registered[b.ID] {
			return result, ErrInvalid
		}
		registered[b.ID] = true
	}
	ordered := append([]KV(nil), values...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Kind != ordered[j].Kind {
			return ordered[i].Kind < ordered[j].Kind
		}
		return ordered[i].ID < ordered[j].ID
	})
	seen := map[string]bool{}
	bound := map[string]ImportedBinding{}
	botPeople := map[string]string{}
	for _, v := range ordered {
		if v.Kind != "contact" && v.Kind != "mention-token" && v.Kind != "sync-group" {
			continue
		}
		key := v.Kind + ":" + v.ID
		if seen[key] || validateJSON([]byte(v.Value), true) != nil {
			return result, ErrInvalid
		}
		seen[key] = true
		switch v.Kind {
		case "contact":
			var source struct {
				Name, Bot        string
				MetadataSyncedAt *int64 `json:"metadataSyncedAt"`
			}
			if !validText(v.ID, 8192) || json.Unmarshal([]byte(v.Value), &source) != nil || (source.MetadataSyncedAt != nil && !validMilliseconds(*source.MetadataSyncedAt)) {
				return result, ErrInvalid
			}
			result.Profiles = append(result.Profiles, identity.Profile{ID: v.ID, Name: source.Name, Synced: source.MetadataSyncedAt != nil && *source.MetadataSyncedAt > 0})
			if source.Bot != "" {
				if !identity.IsPerson(v.ID) {
					return result, ErrInvalid
				}
				if !registered[source.Bot] {
					return result, ErrInvalid
				}
				canonical := directory.Canonical(v.ID)
				if previous, ok := bound[canonical]; ok {
					if previous.BotID != source.Bot {
						return result, ErrInvalid
					}
					continue
				}
				if previous, ok := botPeople[source.Bot]; ok && previous != canonical {
					return result, ErrInvalid
				}
				bound[canonical] = ImportedBinding{PersonID: canonical, BotID: source.Bot, SourceProfile: v.ID}
				botPeople[source.Bot] = canonical
			}
		case "mention-token":
			var source struct{ Session, Chat, Person string }
			if json.Unmarshal([]byte(v.Value), &source) != nil || !copiedToken.MatchString(v.ID) || source.Session != scope || !validWAChat(source.Chat) || !strings.HasSuffix(source.Chat, "@g.us") || !identity.IsPerson(source.Person) {
				return result, ErrInvalid
			}
			result.Tokens[v.ID] = mentions.Token{ChatID: source.Chat, PersonID: identity.Normalize(source.Person)}
		case "sync-group":
			chat, ok := strings.CutPrefix(v.ID, scope+":")
			var source struct {
				Name      string
				FetchedAt int64 `json:"fetchedAt"`
				Members   *[]struct {
					ID, Name     string
					IsAdmin      bool `json:"isAdmin"`
					IsSuperAdmin bool `json:"isSuperAdmin"`
				}
			}
			if !ok || !validWAChat(chat) || !strings.HasSuffix(chat, "@g.us") || json.Unmarshal([]byte(v.Value), &source) != nil || strings.TrimSpace(source.Name) == "" || source.FetchedAt <= 0 || !validMilliseconds(source.FetchedAt) || source.Members == nil {
				return result, ErrInvalid
			}
			roster := mentions.Roster{ChatID: chat, Name: source.Name, FetchedAt: source.FetchedAt, Members: []identity.Member{}}
			for _, member := range *source.Members {
				if !identity.IsPerson(member.ID) {
					return result, ErrInvalid
				}
				roster.Members = append(roster.Members, identity.Member{ID: member.ID, Name: member.Name, IsAdmin: member.IsAdmin, IsSuperAdmin: member.IsSuperAdmin})
			}
			result.Rosters = append(result.Rosters, roster)
		}
	}
	for _, binding := range bound {
		result.Bindings = append(result.Bindings, binding)
	}
	sort.Slice(result.Bindings, func(i, j int) bool { return result.Bindings[i].PersonID < result.Bindings[j].PersonID })
	return result, nil
}
