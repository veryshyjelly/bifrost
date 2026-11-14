package reactions

import (
	"encoding/json"
	"github.com/veryshyjelly/bifrost/internal/control"
	"testing"
)

func TestSnapshotRemovalPreservesOtherActorsAndEventRetries(t *testing.T) {
	state := Reduce(State{}, Event{SenderID: "alice", Reaction: "❤️"}, "first", nil, 1000)
	state = Reduce(state, Event{SenderID: "bob", Reaction: "😂"}, "second", nil, 1001)
	pairs, err := Snapshot(json.RawMessage(`{"alice":"❤️"}`))
	if err != nil {
		t.Fatal(err)
	}
	next := Reduce(state, Event{SenderID: "bob", Snapshot: pairs}, "third", nil, 1002)
	if len(next.Actors) != 1 || next.Actors["alice"].Emoji != "❤" || next.Actors["alice"].Order != state.Actors["alice"].Order {
		t.Fatal(next)
	}
	retry := Reduce(next, Event{SenderID: "bob", Reaction: "👍"}, "third", nil, 1003)
	if len(retry.Actors) != 1 || retry.Revision != next.Revision {
		t.Fatal("same source event changed actor state", retry)
	}
	if state.Actors["bob"].Emoji != "😂" {
		t.Fatal("reducer mutated previous state")
	}
}
func TestReactionDesiredKeepsOtherActorWhenSharedBotActorLeaves(t *testing.T) {
	d := Destination{Key: "group", Target: control.Target{BotID: "master", ChatID: -100}, MessageID: 10}
	state := Reduce(State{}, Event{SenderID: "alice", Reaction: "👍"}, "a", nil, 1000)
	state = Reduce(state, Event{SenderID: "bob", Reaction: "❤"}, "b", nil, 1001)
	selected := Desired(d, state, map[string]string{"alice": "master", "bob": "master"})
	if selected["master"].Actor != "bob" {
		t.Fatal(selected)
	}
	state = Reduce(state, Event{SenderID: "bob"}, "remove", nil, 1002)
	selected = Desired(d, state, nil)
	if selected["master"].Actor != "alice" || selected["master"].Emoji != "👍" {
		t.Fatal("remaining actor was cleared", selected)
	}
}
func TestReactionEchoSuppressionRetainsPrivateBotScope(t *testing.T) {
	echo := Echo{ActorID: "owner", Emoji: "❤️", Target: control.Target{BotID: "200", ChatID: 123}, MessageID: 10, At: 1000}
	state := Reduce(State{}, Event{SenderID: "owner", Reaction: "❤"}, "echo", &echo, 1001)
	d := Destination{Target: echo.Target, MessageID: 10}
	if len(Desired(d, state, nil)) != 0 {
		t.Fatal("native user reaction echoed")
	}
	d.Target.BotID = "201"
	if len(Desired(d, state, nil)) != 1 {
		t.Fatal("private message IDs leaked across bots")
	}
}
func TestReactionSnapshotPreservesProviderOrderAndRejectsMalformedObjects(t *testing.T) {
	pairs, err := Snapshot(json.RawMessage(`{"z":"👍","a":"❤"}`))
	if err != nil || len(*pairs) != 2 || (*pairs)[0].Actor != "z" {
		t.Fatal(pairs, err)
	}
	for _, raw := range []string{`{"a":"👍"} {}`, `{"a":"👍"`} {
		if _, err := Snapshot(json.RawMessage(raw)); err == nil {
			t.Fatal("invalid snapshot accepted", raw)
		}
	}
	if list, err := Snapshot(json.RawMessage(`[]`)); err != nil || list != nil {
		t.Fatal("baseline list became authoritative", list, err)
	}
	if object, err := Snapshot(json.RawMessage(`{"a":3}`)); err != nil || object == nil || len(*object) != 0 {
		t.Fatal("invalid emoji values were not filtered", object, err)
	}
}
