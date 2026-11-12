package legacy

import (
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"

	"github.com/veryshyjelly/bifrost/internal/identity"
)

// ChangeState contains saved source state, never evidence of a new provider
// operation. In particular, a tombstone does not confirm deletion of a copy.
type ChangeState struct {
	Edits    []RetainedEdit
	Outbound []RetainedOutboundEdit
	Revoked  []Completion
}

type RetainedEdit struct {
	ChatID, MessageID, SourceKey, Body string
	MentionedIDs                       []string
}

type RetainedOutboundEdit struct{ ChatID, MessageID, SourceKey, BodyHash string }

// ProjectChanges refuses conflicting alias caches: the legacy latest-edit
// values carry no timestamp with which to establish the newer value.
func ProjectChanges(values []KV, directory *identity.Directory) (ChangeState, error) {
	var result ChangeState
	if directory == nil {
		return result, ErrInvalid
	}
	ordered := append([]KV(nil), values...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Kind != ordered[j].Kind {
			return ordered[i].Kind < ordered[j].Kind
		}
		return ordered[i].ID < ordered[j].ID
	})
	seen := map[[2]string]bool{}
	edits := map[[2]string]int{}
	outbound := map[[2]string]int{}
	revoked := map[[2]string]bool{}
	for _, v := range ordered {
		if v.Kind != "latest-edit" && v.Kind != "outbound-edit" && v.Kind != "revoked" {
			continue
		}
		source := [2]string{v.Kind, v.ID}
		if seen[source] || validateJSON([]byte(v.Value), false) != nil {
			return ChangeState{}, ErrInvalid
		}
		seen[source] = true
		chat, id, err := SplitMessageKey(v.ID)
		if err != nil {
			return ChangeState{}, err
		}
		chat = directory.Canonical(chat)
		key := [2]string{chat, id}
		switch v.Kind {
		case "latest-edit":
			var raw struct {
				Body         *string         `json:"body"`
				MentionedIDs json.RawMessage `json:"mentionedIds"`
			}
			if validateJSON([]byte(v.Value), true) != nil || json.Unmarshal([]byte(v.Value), &raw) != nil || raw.Body == nil {
				return ChangeState{}, ErrInvalid
			}
			e := RetainedEdit{ChatID: chat, MessageID: id, SourceKey: v.ID, Body: *raw.Body}
			if raw.MentionedIDs != nil {
				var ids []json.RawMessage
				if json.Unmarshal(raw.MentionedIDs, &ids) != nil || ids == nil {
					return ChangeState{}, ErrInvalid
				}
				e.MentionedIDs, err = legacyMentionedIDs(raw.MentionedIDs)
				if err != nil {
					return ChangeState{}, err
				}
			}
			if i, exists := edits[key]; exists {
				previous := result.Edits[i]
				if previous.Body != e.Body || !reflect.DeepEqual(previous.MentionedIDs, e.MentionedIDs) {
					return ChangeState{}, ErrInvalid
				}
				continue
			}
			edits[key] = len(result.Edits)
			result.Edits = append(result.Edits, e)
		case "outbound-edit":
			var hash string
			if json.Unmarshal([]byte(v.Value), &hash) != nil || len(hash) != 64 {
				return ChangeState{}, ErrInvalid
			}
			decoded, err := hex.DecodeString(hash)
			if err != nil || hex.EncodeToString(decoded) != hash {
				return ChangeState{}, ErrInvalid
			}
			if i, exists := outbound[key]; exists {
				if result.Outbound[i].BodyHash != hash {
					return ChangeState{}, ErrInvalid
				}
				continue
			}
			outbound[key] = len(result.Outbound)
			result.Outbound = append(result.Outbound, RetainedOutboundEdit{ChatID: chat, MessageID: id, SourceKey: v.ID, BodyHash: hash})
		case "revoked":
			var confirmed *bool
			if json.Unmarshal([]byte(v.Value), &confirmed) != nil || confirmed == nil {
				return ChangeState{}, ErrInvalid
			}
			if *confirmed && !revoked[key] {
				result.Revoked = append(result.Revoked, Completion{ChatID: chat, MessageID: id, SourceKey: v.ID})
				revoked[key] = true
			}
		}
	}
	return result, nil
}
