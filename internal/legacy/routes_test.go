package legacy

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/identity"
)

func retiredValue(chat string, target TopicCoordinate) KV {
	raw, _ := json.Marshal(map[string]string{"chatId": chat})
	return KV{Kind: "retired-topic", ID: legacyTopicKey(target), Value: string(raw)}
}

func TestProjectRoutesPreservesEveryModeDestinationInboxAndGeneration(t *testing.T) {
	components, _ := fixtureComponents(t, "original-account")
	c, err := ParseConfiguration(components["bridge.env"], components["telegram-api.env"])
	if err != nil {
		t.Fatal(err)
	}
	d, err := identity.New([]identity.Alias{{ID: "999999@lid", Canonical: "12025550123@c.us"}}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	values := []KV{
		{Kind: "route", ID: "999999@lid", Value: `{"chatId":"999999@lid","name":"Inbox linked name","tgChat":"-100","thread":7,"mode":"inbox","topicSyncedName":"Previously synced topic"}`},
		{Kind: "route", ID: "12025550124@c.us", Value: `{"chatId":"12025550124@c.us","name":"Private bot","tgChat":"-100","mode":"dedicated","inboxRetired":true}`},
		{Kind: "route", ID: "111@g.us", Value: `{"chatId":"111@g.us","name":"Mirror group","tgChat":"-200","mode":"group","inbox":{"chat":"-100","thread":9},"inboxReturnKey":"original-return-key"}`},
		{Kind: "route", ID: "222@g.us", Value: `{"chatId":"222@g.us","name":"Muted mirror","tgChat":-300,"mode":"muted","inbox":{"chat":"-100","thread":11}}`},
		{Kind: "route", ID: "12025550125@c.us", Value: `{"chatId":"12025550125@c.us","name":"Ignored contact","tgChat":"-100","thread":15,"mode":"ignored"}`},
		retiredValue("222@g.us", TopicCoordinate{Chat: -100, Thread: 11}),
		retiredValue("333@g.us", TopicCoordinate{Chat: -100, Thread: 99}),
	}
	result, err := ProjectRoutes(c, values, d, []TopicCoordinate{{Chat: -100, Thread: 99}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Routes) != 5 || len(result.Retired) != 2 || len(result.Inboxes) != 5 {
		t.Fatal("route types or permanent inbox evidence lost")
	}
	byID := map[string]ImportedRoute{}
	for _, r := range result.Routes {
		byID[r.Route.ChatID] = r
	}
	if byID["12025550123@c.us"].Route.Mode != conversation.Inbox || byID["12025550123@c.us"].Route.ThreadID != 7 || byID["12025550123@c.us"].TopicName != "Previously synced topic" {
		t.Fatal("verified alias route or original topic-name cache changed")
	}
	if r := byID["12025550124@c.us"]; r.Route.Mode != conversation.Dedicated || !r.Route.InboxRetired || r.Route.ThreadID != 0 {
		t.Fatal("private route resurrected a retired inbox")
	}
	if r := byID["111@g.us"]; r.Route.Mode != conversation.Group || r.Route.TelegramChatID != -200 || r.ReturnKey != "original-return-key" {
		t.Fatal("mirror destination or return generation changed")
	}
	if byID["222@g.us"].Route.Mode != conversation.Muted || byID["12025550125@c.us"].Route.Mode != conversation.Ignored {
		t.Fatal("paused modes changed")
	}
}

func TestProjectRoutesPreservesOwnedRetirementCrashAndBlocksMissingOrConflictingEvidence(t *testing.T) {
	components, _ := fixtureComponents(t, "original-account")
	c, err := ParseConfiguration(components["bridge.env"], components["telegram-api.env"])
	if err != nil {
		t.Fatal(err)
	}
	d, _ := identity.New(nil, nil, nil, nil)
	route := KV{Kind: "route", ID: "111@g.us", Value: `{"chatId":"111@g.us","tgChat":"-100","thread":7,"mode":"inbox"}`}
	retired := retiredValue("111@g.us", TopicCoordinate{Chat: -100, Thread: 7})
	result, err := ProjectRoutes(c, []KV{route, retired}, d, nil)
	if err != nil || len(result.Routes) != 1 || !result.Routes[0].PendingRetirement || result.Routes[0].Route.ThreadID != 7 || result.Routes[0].Route.InboxRetired {
		t.Fatal("retirement crash rewrote source route or lost its permanent fence")
	}
	if _, err := ProjectRoutes(c, []KV{retired}, d, nil); !errors.Is(err, ErrTopicEvidence) {
		t.Fatal("opaque retired-topic reservation silently dropped")
	}
	foreign := retiredValue("222@g.us", TopicCoordinate{Chat: -100, Thread: 7})
	if _, err := ProjectRoutes(c, []KV{route, foreign}, d, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("retired topic assigned to another conversation")
	}
}

func TestProjectRoutesRefusesAmbiguousDestinationsAliasesAndInboxOwnership(t *testing.T) {
	components, _ := fixtureComponents(t, "original-account")
	c, err := ParseConfiguration(components["bridge.env"], components["telegram-api.env"])
	if err != nil {
		t.Fatal(err)
	}
	d, _ := identity.New([]identity.Alias{{ID: "999999@lid", Canonical: "12025550123@c.us"}}, nil, nil, nil)
	for _, values := range [][]KV{
		{{Kind: "route", ID: "111@g.us", Value: `{"chatId":"111@g.us","tgChat":"-200","mode":"group"}`}, {Kind: "route", ID: "222@g.us", Value: `{"chatId":"222@g.us","tgChat":"-200","mode":"muted"}`}},
		{{Kind: "route", ID: "111@g.us", Value: `{"chatId":"111@g.us","tgChat":"-200","mode":"group","inbox":{"chat":"-100","thread":7}}`}, {Kind: "route", ID: "222@g.us", Value: `{"chatId":"222@g.us","tgChat":"-300","mode":"group","inbox":{"chat":"-100","thread":7}}`}},
		{{Kind: "route", ID: "999999@lid", Value: `{"chatId":"999999@lid","tgChat":"-100","thread":7}`}, {Kind: "route", ID: "12025550123@c.us", Value: `{"chatId":"12025550123@c.us","tgChat":"-100","thread":9}`}},
		{{Kind: "route", ID: "111@g.us", Value: `{"chatId":"222@g.us","tgChat":"-100","thread":7}`}},
		{{Kind: "route", ID: "111@g.us", Value: `{"chatId":"111@g.us","tgChat":"-100","thread":1}`}},
		{{Kind: "route", ID: "111@g.us", Value: `{"chatId":"111@g.us","tgChat":"123","thread":7}`}},
		{{Kind: "route", ID: "111@g.us", Value: `{"chatId":"111@g.us","tgChat":"-100","mode":"unsupported"}`}},
	} {
		if _, err := ProjectRoutes(c, values, d, nil); err == nil {
			t.Fatal("ambiguous route projection accepted")
		}
	}
}
