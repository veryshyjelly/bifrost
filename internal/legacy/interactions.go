package legacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"

	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/reactions"
)

type PollSelection struct {
	ChatID, MessageID, SourceKey string
	Options                      []string
}
type CustomReaction struct{ ID, Emoji string }
type ReactionChoice struct {
	SourceKey string
	Choice    reactions.FrozenChoice
}
type InteractionState struct {
	Polls   []PollSelection
	Custom  []CustomReaction
	Choices []ReactionChoice
}

// OutgoingReactionKey reproduces JSON.stringify's string tuple exactly,
// including HTML characters and Unicode separators in an original account.
// Its value identifies a saved resolution, not a confirmed provider mutation.
func OutgoingReactionKey(account, bot, key string) string {
	return stringTupleKey(account, bot, key)
}

func stringTupleKey(values ...string) string {
	const digits = "0123456789abcdef"
	raw := []byte{'['}
	for i, value := range values {
		if i > 0 {
			raw = append(raw, ',')
		}
		raw = append(raw, '"')
		for _, r := range value {
			switch r {
			case '"', '\\':
				raw = append(raw, '\\', byte(r))
			case '\b':
				raw = append(raw, '\\', 'b')
			case '\f':
				raw = append(raw, '\\', 'f')
			case '\n':
				raw = append(raw, '\\', 'n')
			case '\r':
				raw = append(raw, '\\', 'r')
			case '\t':
				raw = append(raw, '\\', 't')
			default:
				if r < 32 {
					raw = append(raw, '\\', 'u', '0', '0', digits[r>>4], digits[r&15])
				} else {
					raw = append(raw, string(r)...)
				}
			}
		}
		raw = append(raw, '"')
	}
	raw = append(raw, ']')
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func ProjectInteractions(values []KV, directory *identity.Directory) (InteractionState, error) {
	var result InteractionState
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
	polls := map[[2]string]int{}
	for _, value := range ordered {
		if value.Kind != "poll-selection" && value.Kind != "custom-reaction-emoji" && value.Kind != "outgoing-reaction" {
			continue
		}
		key := [2]string{value.Kind, value.ID}
		if seen[key] || validateJSON([]byte(value.Value), false) != nil {
			return InteractionState{}, ErrInvalid
		}
		seen[key] = true
		switch value.Kind {
		case "poll-selection":
			chat, id, err := SplitMessageKey(value.ID)
			if err != nil {
				return InteractionState{}, err
			}
			var selected []*string
			if json.Unmarshal([]byte(value.Value), &selected) != nil || selected == nil {
				return InteractionState{}, ErrInvalid
			}
			options := make([]string, len(selected))
			for i, option := range selected {
				if option == nil {
					return InteractionState{}, ErrInvalid
				}
				options[i] = *option
			}
			chat = directory.Canonical(chat)
			key := [2]string{chat, id}
			if i, exists := polls[key]; exists {
				if !reflect.DeepEqual(result.Polls[i].Options, options) {
					return InteractionState{}, ErrInvalid
				}
				continue
			}
			polls[key] = len(result.Polls)
			result.Polls = append(result.Polls, PollSelection{ChatID: chat, MessageID: id, SourceKey: value.ID, Options: options})
		case "custom-reaction-emoji":
			var cached struct{ Emoji string }
			if !validText(value.ID, 8192) || validateJSON([]byte(value.Value), true) != nil || json.Unmarshal([]byte(value.Value), &cached) != nil || !reactions.EmojiEquivalent(cached.Emoji) {
				return InteractionState{}, ErrInvalid
			}
			result.Custom = append(result.Custom, CustomReaction{ID: value.ID, Emoji: cached.Emoji})
		case "outgoing-reaction":
			var cached struct {
				Emoji       *string
				Unsupported *bool
			}
			digest, err := hex.DecodeString(value.ID)
			if err != nil || len(digest) != 32 || hex.EncodeToString(digest) != value.ID || validateJSON([]byte(value.Value), true) != nil || json.Unmarshal([]byte(value.Value), &cached) != nil {
				return InteractionState{}, ErrInvalid
			}
			choice := reactions.FrozenChoice{}
			if cached.Emoji != nil && cached.Unsupported == nil {
				choice.Emoji = *cached.Emoji
				choice.Supported = true
				if choice.Emoji != "" && !reactions.EmojiEquivalent(choice.Emoji) {
					return InteractionState{}, ErrInvalid
				}
			} else if cached.Emoji != nil || cached.Unsupported == nil || !*cached.Unsupported {
				return InteractionState{}, ErrInvalid
			}
			result.Choices = append(result.Choices, ReactionChoice{SourceKey: value.ID, Choice: choice})
		}
	}
	return result, nil
}
