package reactions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/veryshyjelly/bifrost/internal/control"
)

type Echo struct {
	ActorID, Emoji string
	Target         control.Target
	MessageID, At  int64
}
type Actor struct {
	Emoji string
	Order int64
	Echo  *Echo
}
type State struct {
	Event    string
	Revision int64
	Actors   map[string]Actor
	Known    map[string]bool
	Full     bool
}
type Pair struct{ Actor, Emoji string }
type Event struct {
	Session, ChatID, MessageID, SenderID, Reaction string
	Snapshot                                       *[]Pair
}

// Snapshot preserves the provider's object order so aliases and new actors
// have deterministic revisions rather than Go map iteration order.
func Snapshot(raw json.RawMessage) (*[]Pair, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		// Older providers may include a list or another non-object value.
		// As in the baseline, only an object is an authoritative snapshot.
		if !json.Valid(raw) {
			return nil, fmt.Errorf("invalid reaction snapshot")
		}
		return nil, nil
	}
	result := []Pair{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		actor, ok := key.(string)
		if !ok {
			return nil, fmt.Errorf("invalid reaction actor")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		var emoji string
		if json.Unmarshal(value, &emoji) != nil {
			continue
		}
		result = append(result, Pair{Actor: actor, Emoji: emoji})
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing reaction snapshot data")
	}
	return &result, nil
}

// Reduce is pure: retries of the same event retain actor ordering, and a full
// snapshot removes absent actors without clearing those who remain.
func Reduce(previous State, event Event, key string, echo *Echo, now int64) State {
	if previous.Event == key {
		return previous
	}
	next := State{Event: key, Revision: previous.Revision, Actors: map[string]Actor{}, Known: map[string]bool{}, Full: previous.Full}
	for id, value := range previous.Known {
		next.Known[id] = value
	}
	for id, actor := range previous.Actors {
		next.Actors[id] = actor
	}
	if event.Snapshot != nil {
		next.Full = true
		next.Actors = map[string]Actor{}
		for _, pair := range *event.Snapshot {
			next.Known[pair.Actor] = true
			emoji := EmojiKey(pair.Emoji)
			if emoji == "" {
				continue
			}
			old := previous.Actors[pair.Actor]
			if old.Emoji == emoji {
				next.Actors[pair.Actor] = old
			} else {
				next.Revision++
				next.Actors[pair.Actor] = Actor{Emoji: emoji, Order: next.Revision}
			}
		}
	}
	if event.SenderID == "" {
		return next
	}
	next.Known[event.SenderID] = true
	emoji := EmojiKey(event.Reaction)
	if emoji == "" {
		delete(next.Actors, event.SenderID)
		return next
	}
	var represented *Echo
	if echo != nil && echo.ActorID == event.SenderID && EmojiKey(echo.Emoji) == emoji && now >= echo.At && now-echo.At < 120000 {
		copy := *echo
		represented = &copy
	} else if old := previous.Actors[event.SenderID]; old.Emoji == emoji {
		represented = old.Echo
	}
	next.Revision++
	next.Actors[event.SenderID] = Actor{Emoji: emoji, Order: next.Revision, Echo: represented}
	return next
}

type Applied struct {
	MessageID    int64
	Emoji, Actor string
	Order        int64
}
type Refused struct {
	MessageID     int64
	Emoji, Reason string
	At            int64
}
type Cache struct {
	Applied map[string]Applied
	Refused map[string]Refused
}
type Destination struct {
	Key       string
	Target    control.Target
	MessageID int64
}
type Choice struct {
	Actor, Emoji, BotID string
	Order               int64
}
type Group struct {
	Destination Destination
	Desired     map[string]Choice
	Unavailable bool
}

func Desired(destination Destination, state State, bots map[string]string) map[string]Choice {
	desired := map[string]Choice{}
	for id, actor := range state.Actors {
		if actor.Echo != nil && actor.Echo.Target.ChatID == destination.Target.ChatID && actor.Echo.MessageID == destination.MessageID && (destination.Target.ChatID < 0 || actor.Echo.Target.BotID == destination.Target.BotID) {
			continue
		}
		bot := bots[id]
		if destination.Target.ChatID > 0 {
			bot = destination.Target.BotID
		}
		if bot == "" {
			bot = "master"
		}
		candidate := Choice{Actor: id, Emoji: actor.Emoji, BotID: bot, Order: actor.Order}
		if prior, exists := desired[bot]; !exists || prior.Order < candidate.Order {
			desired[bot] = candidate
		}
	}
	return desired
}
