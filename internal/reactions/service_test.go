package reactions

import (
	"testing"

	"github.com/veryshyjelly/bifrost/internal/control"
)

func TestPlanForgetsUnappliedRefusalWithoutProviderSetter(t *testing.T) {
	d := Destination{Key: "*:-100:44", Target: control.Target{BotID: "master", ChatID: -100, ThreadID: 44}, MessageID: 11}
	in := Input{State: State{Actors: map[string]Actor{}}, Destinations: []Destination{d}, Caches: map[string]Cache{d.Key: {Refused: map[string]Refused{"master": {MessageID: 11, Emoji: "👍", At: 100}}}}}
	p := buildPlan("event", in, nil, 60, 101)
	if len(p.Steps) != 1 || p.Steps[0].Kind != "forget-refusal" || p.Steps[0].BotID != "master" {
		t.Fatal("removed actor retained refusal or queued a provider setter")
	}
	if len(in.Caches[d.Key].Refused) != 1 {
		t.Fatal("planning mutated its input before persistence")
	}
}

func TestPlanPreservesMemberRefusalUsedForMasterFallback(t *testing.T) {
	d := Destination{Key: "*:-100:44", Target: control.Target{BotID: "master", ChatID: -100, ThreadID: 44}, MessageID: 11}
	in := Input{State: State{Actors: map[string]Actor{"actor": {Emoji: "👍", Order: 1}}}, Destinations: []Destination{d}, Caches: map[string]Cache{d.Key: {Refused: map[string]Refused{"77": {MessageID: 11, Emoji: "👍", Reason: "message_missing", At: 100}}}}}
	p := buildPlan("event", in, map[string]map[string]string{d.Key: {"actor": "77"}}, 60, 101)
	if len(p.Steps) != 1 || p.Steps[0].Kind != "set" || p.Steps[0].BotID != "master" {
		t.Fatal("master fallback lost its member refusal cache")
	}
}
