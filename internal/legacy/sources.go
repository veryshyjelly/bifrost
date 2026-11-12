package legacy

import (
	"encoding/json"
	"sort"

	"github.com/veryshyjelly/bifrost/internal/identity"
)

// RetainedSource is the identity/context subset the legacy wamessage journal
// actually retained. Body and quoted content are deliberately absent: they
// require an original event or a frozen plan, not invented empty content.
type RetainedSource struct {
	ChatID, RawChatID, MessageID, SenderID, LinkSenderID, Name string
	Own, Historical, Status                                    bool
	Timestamp                                                  int64
	MentionedIDs                                               []string
	SourceKey                                                  string `json:"-"`
}

func ProjectSources(values []KV, directory *identity.Directory) ([]RetainedSource, error) {
	if directory == nil {
		return nil, ErrInvalid
	}
	ordered := append([]KV(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	byMessage := map[[2]string]RetainedSource{}
	seen := map[string]bool{}
	for _, v := range ordered {
		if v.Kind != "wamessage" {
			continue
		}
		if seen[v.ID] || validateJSON([]byte(v.Value), true) != nil {
			return nil, ErrInvalid
		}
		seen[v.ID] = true
		chat, id, err := SplitMessageKey(v.ID)
		if err != nil {
			return nil, err
		}
		var source struct {
			Sender, LinkSender, Name string
			FromMe                   bool `json:"fromMe"`
			Historical, Status       bool
			Timestamp                int64
			MentionedIDs             json.RawMessage `json:"mentionedIds"`
		}
		if json.Unmarshal([]byte(v.Value), &source) != nil || source.Timestamp < 0 || (source.Sender != "" && !validText(source.Sender, 8192)) || (source.LinkSender != "" && !validText(source.LinkSender, 8192)) {
			return nil, ErrInvalid
		}
		mentioned, err := legacyMentionedIDs(source.MentionedIDs)
		if err != nil {
			return nil, err
		}
		canonical := directory.Canonical(chat)
		value := RetainedSource{ChatID: canonical, RawChatID: chat, MessageID: id, SenderID: directory.Canonical(source.Sender), LinkSenderID: directory.Canonical(source.LinkSender), Name: source.Name, Own: source.FromMe, Historical: source.Historical, Status: source.Status, Timestamp: source.Timestamp, MentionedIDs: mentioned, SourceKey: v.ID}
		key := [2]string{canonical, id}
		if old, exists := byMessage[key]; exists {
			if old.SenderID != value.SenderID || old.Own != value.Own {
				return nil, ErrInvalid
			}
			// The legacy canonical lookup selects the exact canonical key when
			// both an older alias and that key remain in the frozen source.
			if old.RawChatID == canonical || chat != canonical {
				continue
			}
		}
		byMessage[key] = value
	}
	result := make([]RetainedSource, 0, len(byMessage))
	for _, source := range byMessage {
		result = append(result, source)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ChatID != result[j].ChatID {
			return result[i].ChatID < result[j].ChatID
		}
		return result[i].MessageID < result[j].MessageID
	})
	return result, nil
}
