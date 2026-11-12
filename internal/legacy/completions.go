package legacy

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/veryshyjelly/bifrost/internal/identity"
)

type Completion struct{ ChatID, MessageID, SourceKey string }

// SplitMessageKey preserves device and message-ID colons. Legacy keys use a
// complete WhatsApp JID followed by a colon, rather than an escaped tuple.
func SplitMessageKey(key string) (chat, message string, err error) {
	for _, suffix := range []string{"@c.us:", "@s.whatsapp.net:", "@lid:", "@g.us:", "status@broadcast:"} {
		i := strings.Index(key, suffix)
		if i < 0 {
			continue
		}
		end := i + len(suffix) - 1
		chat, message = key[:end], key[end+1:]
		if validWAChat(chat) && validText(message, 8192) {
			return chat, message, nil
		}
	}
	return "", "", ErrInvalid
}

// ProjectCompletions uses only explicit true source markers. A mapping or
// uncertain effect never proves completion. History-all copies remain a
// separate scoped workflow and do not use this live-delivery guard.
func ProjectCompletions(values []KV, directory *identity.Directory) ([]Completion, error) {
	if directory == nil {
		return nil, ErrInvalid
	}
	ordered := append([]KV(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	seen := map[string]bool{}
	completed := map[[2]string]bool{}
	var result []Completion
	for _, v := range ordered {
		if v.Kind != "wa-complete" {
			continue
		}
		if seen[v.ID] || validateJSON([]byte(v.Value), false) != nil {
			return nil, ErrInvalid
		}
		seen[v.ID] = true
		var done *bool
		if json.Unmarshal([]byte(v.Value), &done) != nil || done == nil {
			return nil, ErrInvalid
		}
		chat, id, err := SplitMessageKey(v.ID)
		if err != nil {
			return nil, err
		}
		if !*done {
			continue
		}
		chat = directory.Canonical(chat)
		key := [2]string{chat, id}
		if completed[key] {
			continue
		}
		completed[key] = true
		result = append(result, Completion{ChatID: chat, MessageID: id, SourceKey: v.ID})
	}
	return result, nil
}
