package legacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/reactions"
)

var ErrReactionEvidence = errors.New("legacy reaction state lacks scoped retained evidence")

type ReactionCopy struct {
	RawChatID, MessageID, BotID                   string
	TelegramChat, TelegramMessage, TelegramThread int64
}
type ReactionEvidence struct {
	Values []KV
	Jobs   []Job
	Copies []ReactionCopy
	Bots   []string
}
type RetainedReactionState struct {
	ChatID, MessageID, SourceKey string
	State                        reactions.State
}
type RetainedReactionEcho struct {
	ChatID, MessageID, SourceKey string
	Echo                         reactions.Echo
}
type RetainedReactionCache struct {
	ChatID, MessageID, SourceKey, DestinationKey, PendingFallback string
	Cache                                                         reactions.Cache
}
type IncomingReactionState struct {
	States []RetainedReactionState
	Echoes []RetainedReactionEcho
	Caches []RetainedReactionCache
}
type reactionCoordinate struct{ chat, message string }
type reactionCopyKey struct {
	bot           string
	chat, message int64
}

func reactionSourceKey(account, chat, message string) string {
	// Both legacy keys are hashes of a three-string JavaScript JSON tuple.
	return OutgoingReactionKey(account, chat, message)
}

// ProjectIncomingReactions restores only retained facts. Opaque keys must match
// original message/job coordinates and original Telegram copy locations.
func ProjectIncomingReactions(account string, evidence ReactionEvidence, directory *identity.Directory) (IncomingReactionState, error) {
	var result IncomingReactionState
	if !validAccount(account) || directory == nil {
		return result, ErrInvalid
	}
	bySource := map[string]reactionCoordinate{}
	hashes := map[reactionCoordinate]map[string]bool{}
	add := func(chat, message string) error {
		if !validWAChat(chat) || !validText(message, 8192) {
			return ErrInvalid
		}
		canonical := directory.Canonical(chat)
		coordinate := reactionCoordinate{canonical, message}
		if hashes[coordinate] == nil {
			hashes[coordinate] = map[string]bool{}
		}
		for _, original := range []string{chat, canonical} {
			key := reactionSourceKey(account, original, message)
			if previous, exists := bySource[key]; exists && previous != coordinate {
				return ErrReactionEvidence
			}
			bySource[key] = coordinate
			hashes[coordinate][key] = true
		}
		return nil
	}
	bots := map[string]bool{"master": true}
	for _, bot := range evidence.Bots {
		if !validText(bot, 128) {
			return result, ErrInvalid
		}
		bots[bot] = true
	}
	copies := map[reactionCopyKey]ReactionCopy{}
	for _, copy := range evidence.Copies {
		if !bots[copy.BotID] || !validTelegramChat(strconv.FormatInt(copy.TelegramChat, 10)) || !validMessageID(copy.TelegramMessage) || copy.TelegramThread < 0 || copy.TelegramThread == 1 || copy.TelegramThread > 2147483647 || (copy.TelegramChat > 0 && copy.TelegramThread != 0) {
			return result, ErrInvalid
		}
		if err := add(copy.RawChatID, copy.MessageID); err != nil {
			return result, err
		}
		key := reactionCopyKey{copy.BotID, copy.TelegramChat, copy.TelegramMessage}
		if _, exists := copies[key]; exists {
			return result, ErrInvalid
		}
		copies[key] = copy
	}
	for _, value := range evidence.Values {
		switch value.Kind {
		case "wamessage", "latest-edit", "revoked", "wa-complete", "poll-selection":
			chat, message, err := SplitMessageKey(value.ID)
			if err != nil {
				return result, err
			}
			if err := add(chat, message); err != nil {
				return result, err
			}
		}
	}
	pendingJobs := map[string]reactionCoordinate{}
	for _, job := range evidence.Jobs {
		if job.Type != "wa" {
			continue
		}
		var event struct {
			Event string
			Data  struct{ ChatID, ID, MessageID, RevokedID, PollMessageID string }
		}
		if json.Unmarshal([]byte(job.Payload), &event) != nil {
			return result, ErrInvalid
		}
		message := event.Data.RevokedID
		if message == "" {
			message = event.Data.PollMessageID
		}
		if message == "" {
			message = event.Data.MessageID
		}
		if message == "" {
			message = event.Data.ID
		}
		if event.Data.ChatID != "" && message != "" {
			if err := add(event.Data.ChatID, message); err != nil {
				return result, err
			}
			if event.Event == "message.reaction" {
				pendingJobs[job.Dedup] = reactionCoordinate{directory.Canonical(event.Data.ChatID), message}
			}
		}
	}
	type destination struct {
		coordinate reactionCoordinate
		key        string
	}
	destinations := map[string]destination{}
	targets := map[destination]map[int64]bool{}
	for _, copy := range evidence.Copies {
		coordinate := reactionCoordinate{directory.Canonical(copy.RawChatID), copy.MessageID}
		scope := telegramScope(copy.BotID, copy.TelegramChat)
		key := fmt.Sprintf("%s:%d:%d", scope, copy.TelegramChat, copy.TelegramThread)
		d := destination{coordinate, key}
		if targets[d] == nil {
			targets[d] = map[int64]bool{}
		}
		targets[d][copy.TelegramMessage] = true
		sum := sha256.Sum256([]byte(key))
		for source := range hashes[coordinate] {
			destinations[source+":"+hex.EncodeToString(sum[:])] = destination{coordinate, key}
		}
	}
	resolveEcho := func(raw json.RawMessage) (reactions.Echo, error) {
		var saved struct {
			Sender, Bot, Chat, Emoji string
			Message, At              int64
		}
		if validateJSON(raw, true) != nil || json.Unmarshal(raw, &saved) != nil || !validText(saved.Sender, 8192) || !bots[saved.Bot] || !validTelegramChat(saved.Chat) || !validMessageID(saved.Message) || saved.At < 0 || saved.At > 9007199254740991 {
			return reactions.Echo{}, ErrInvalid
		}
		chat, _ := strconv.ParseInt(saved.Chat, 10, 64)
		copy, found := copies[reactionCopyKey{saved.Bot, chat, saved.Message}]
		if !found {
			return reactions.Echo{}, ErrReactionEvidence
		}
		return reactions.Echo{ActorID: directory.Canonical(saved.Sender), Emoji: saved.Emoji, Target: control.Target{BotID: saved.Bot, ChatID: chat, ThreadID: copy.TelegramThread}, MessageID: saved.Message, At: saved.At}, nil
	}
	ordered := append([]KV(nil), evidence.Values...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Kind != ordered[j].Kind {
			return ordered[i].Kind < ordered[j].Kind
		}
		return ordered[i].ID < ordered[j].ID
	})
	seen := map[[2]string]bool{}
	states := map[reactionCoordinate]int{}
	echoes := map[reactionCoordinate]int{}
	caches := map[[3]string]int{}
	for _, value := range ordered {
		if value.Kind != "wa-reaction-state" && value.Kind != "reaction-echo" && value.Kind != "tg-reaction-state" {
			continue
		}
		key := [2]string{value.Kind, value.ID}
		if seen[key] || validateJSON([]byte(value.Value), true) != nil {
			return IncomingReactionState{}, ErrInvalid
		}
		seen[key] = true
		if value.Kind == "tg-reaction-state" {
			destination, found := destinations[value.ID]
			if !found {
				return IncomingReactionState{}, ErrReactionEvidence
			}
			cache, pending, err := projectReactionCache([]byte(value.Value), bots)
			if err != nil {
				return IncomingReactionState{}, err
			}
			if pending != "" && pendingJobs[pending] != destination.coordinate {
				return IncomingReactionState{}, ErrReactionEvidence
			}
			for bot, applied := range cache.Applied {
				if !targets[destination][applied.MessageID] || (destination.key[0] != '*' && !strings.HasPrefix(destination.key, bot+":")) {
					return IncomingReactionState{}, ErrReactionEvidence
				}
			}
			for bot, refused := range cache.Refused {
				if !targets[destination][refused.MessageID] || (destination.key[0] != '*' && !strings.HasPrefix(destination.key, bot+":")) {
					return IncomingReactionState{}, ErrReactionEvidence
				}
			}
			key := [3]string{destination.coordinate.chat, destination.coordinate.message, destination.key}
			if i, exists := caches[key]; exists {
				previous := result.Caches[i]
				if !reflect.DeepEqual(previous.Cache, cache) || previous.PendingFallback != pending {
					return IncomingReactionState{}, ErrReactionEvidence
				}
				continue
			}
			caches[key] = len(result.Caches)
			result.Caches = append(result.Caches, RetainedReactionCache{ChatID: destination.coordinate.chat, MessageID: destination.coordinate.message, SourceKey: value.ID, DestinationKey: destination.key, PendingFallback: pending, Cache: cache})
			continue
		}
		coordinate, found := bySource[value.ID]
		if !found {
			return IncomingReactionState{}, ErrReactionEvidence
		}
		if value.Kind == "reaction-echo" {
			echo, err := resolveEcho([]byte(value.Value))
			if err != nil {
				return IncomingReactionState{}, err
			}
			copy := copies[reactionCopyKey{echo.Target.BotID, echo.Target.ChatID, echo.MessageID}]
			if directory.Canonical(copy.RawChatID) != coordinate.chat || copy.MessageID != coordinate.message {
				return IncomingReactionState{}, ErrReactionEvidence
			}
			if i, exists := echoes[coordinate]; exists {
				if !reflect.DeepEqual(result.Echoes[i].Echo, echo) {
					return IncomingReactionState{}, ErrReactionEvidence
				}
				continue
			}
			echoes[coordinate] = len(result.Echoes)
			result.Echoes = append(result.Echoes, RetainedReactionEcho{ChatID: coordinate.chat, MessageID: coordinate.message, SourceKey: value.ID, Echo: echo})
			continue
		}
		var saved struct {
			Senders  map[string]json.RawMessage
			Revision *int64
			Event    string
		}
		if json.Unmarshal([]byte(value.Value), &saved) != nil || saved.Senders == nil || saved.Revision == nil || *saved.Revision < 0 || *saved.Revision > 9007199254740991 || (saved.Event != "" && !validText(saved.Event, 8192)) {
			return IncomingReactionState{}, ErrInvalid
		}
		state := reactions.State{Event: saved.Event, Revision: *saved.Revision, Actors: map[string]reactions.Actor{}, Known: map[string]bool{}}
		orders := map[int64]string{}
		for actor, raw := range saved.Senders {
			var old struct {
				Emoji *string
				Order *int64
				Echo  json.RawMessage
			}
			if !validText(actor, 8192) || json.Unmarshal(raw, &old) != nil || old.Emoji == nil || *old.Emoji == "" || old.Order == nil || *old.Order <= 0 || *old.Order > state.Revision {
				return IncomingReactionState{}, ErrInvalid
			}
			actor = directory.Canonical(actor)
			item := reactions.Actor{Emoji: *old.Emoji, Order: *old.Order}
			if len(old.Echo) > 0 && string(old.Echo) != "null" {
				echo, err := resolveEcho(old.Echo)
				if err != nil {
					return IncomingReactionState{}, err
				}
				copy := copies[reactionCopyKey{echo.Target.BotID, echo.Target.ChatID, echo.MessageID}]
				if directory.Canonical(copy.RawChatID) != coordinate.chat || copy.MessageID != coordinate.message {
					return IncomingReactionState{}, ErrReactionEvidence
				}
				item.Echo = &echo
			}
			if prior, exists := orders[item.Order]; exists && prior != actor {
				return IncomingReactionState{}, ErrReactionEvidence
			}
			orders[item.Order] = actor
			if prior, exists := state.Actors[actor]; exists {
				// Unlike separate alias caches, actor orders in one state share
				// the same revision counter and establish a reliable winner.
				if prior.Order > item.Order {
					continue
				}
				if prior.Order == item.Order && !reflect.DeepEqual(prior, item) {
					return IncomingReactionState{}, ErrReactionEvidence
				}
			}
			state.Actors[actor] = item
			state.Known[actor] = true
		}
		if i, exists := states[coordinate]; exists {
			if !reflect.DeepEqual(result.States[i].State, state) {
				return IncomingReactionState{}, ErrReactionEvidence
			}
			continue
		}
		states[coordinate] = len(result.States)
		result.States = append(result.States, RetainedReactionState{ChatID: coordinate.chat, MessageID: coordinate.message, SourceKey: value.ID, State: state})
	}
	return result, nil
}

func projectReactionCache(raw []byte, bots map[string]bool) (reactions.Cache, string, error) {
	cache := reactions.Cache{Applied: map[string]reactions.Applied{}, Refused: map[string]reactions.Refused{}}
	var saved struct {
		Applied, Refused map[string]string
		RefusedAt        map[string]int64
		RefusedReason    map[string]string
		PendingFallback  string
	}
	if json.Unmarshal(raw, &saved) != nil || saved.Applied == nil || saved.Refused == nil || (saved.PendingFallback != "" && !validText(saved.PendingFallback, 8192)) {
		return cache, "", ErrInvalid
	}
	decode := func(raw string) (int64, string, error) {
		var pair []json.RawMessage
		if validateJSON([]byte(raw), false) != nil || json.Unmarshal([]byte(raw), &pair) != nil || len(pair) != 2 {
			return 0, "", ErrInvalid
		}
		var message int64
		var emoji *string
		if json.Unmarshal(pair[0], &message) != nil || !validMessageID(message) || json.Unmarshal(pair[1], &emoji) != nil || emoji == nil {
			return 0, "", ErrInvalid
		}
		return message, *emoji, nil
	}
	for bot, signature := range saved.Applied {
		message, emoji, err := decode(signature)
		if !bots[bot] || err != nil {
			return cache, "", ErrInvalid
		}
		cache.Applied[bot] = reactions.Applied{MessageID: message, Emoji: emoji}
	}
	for bot, signature := range saved.Refused {
		message, emoji, err := decode(signature)
		at := saved.RefusedAt[bot]
		if !bots[bot] || err != nil || at < 0 || at > 9007199254740991 {
			return cache, "", ErrInvalid
		}
		cache.Refused[bot] = reactions.Refused{MessageID: message, Emoji: emoji, At: at, Reason: saved.RefusedReason[bot]}
	}
	return cache, saved.PendingFallback, nil
}
