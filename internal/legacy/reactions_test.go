package legacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/identity"
)

func legacyReactionDestinationKey(account, chat, message, destination string) string {
	sum := sha256.Sum256([]byte(destination))
	return reactionSourceKey(account, chat, message) + ":" + hex.EncodeToString(sum[:])
}

func legacyIncomingReactionEvidence() ReactionEvidence {
	echo := `{"sender":"12025550999@c.us","bot":"master","chat":"-100","message":10,"emoji":"👍","at":1700000000000}`
	return ReactionEvidence{
		Bots: []string{"master", "77"},
		Copies: []ReactionCopy{
			{RawChatID: "12345@g.us", MessageID: "source", BotID: "master", TelegramChat: -100, TelegramMessage: 10, TelegramThread: 44},
			{RawChatID: "999999@lid", MessageID: "private", BotID: "77", TelegramChat: 123, TelegramMessage: 42},
			{RawChatID: "12025550123@c.us", MessageID: "private", BotID: "master", TelegramChat: 123, TelegramMessage: 42},
		},
		Jobs: []Job{{Type: "wa", Dedup: "ow:reaction", Payload: `{"event":"message.reaction","data":{"chatId":"12345@g.us","messageId":"source"}}`}},
		Values: []KV{
			{Kind: "wa-reaction-state", ID: reactionSourceKey("account", "12345@g.us", "source"), Value: `{"event":"ow:previous","revision":5,"senders":{"999999@lid":{"emoji":"👍","order":2,"echo":` + echo + `},"888888@lid":{"emoji":"❤","order":4}}}`},
			{Kind: "reaction-echo", ID: reactionSourceKey("account", "12345@g.us", "source"), Value: echo},
			{Kind: "tg-reaction-state", ID: legacyReactionDestinationKey("account", "12345@g.us", "source", "*:-100:44"), Value: `{"applied":{"master":"[10,\"👍\"]"},"refused":{"77":"[10,\"👎\"]"},"refusedAt":{"77":1700000000123},"refusedReason":{"77":"message_missing"},"pendingFallback":"ow:reaction"}`},
			{Kind: "tg-reaction-state", ID: legacyReactionDestinationKey("account", "999999@lid", "private", "77:123:0"), Value: `{"applied":{"77":"[42,\"👍\"]"},"refused":{}}`},
			{Kind: "tg-reaction-state", ID: legacyReactionDestinationKey("account", "12025550123@c.us", "private", "master:123:0"), Value: `{"applied":{"master":"[42,\"👎\"]"},"refused":{}}`},
			{Kind: "reaction-echo", ID: reactionSourceKey("account", "999999@lid", "private"), Value: `{"sender":"12025550999@c.us","bot":"77","chat":"123","message":42,"emoji":"👍","at":1700000000000}`},
		},
	}
}

func TestLegacyIncomingReactionsPreserveFactsOrderingAndPrivateCopyScope(t *testing.T) {
	d, _ := identity.New([]identity.Alias{{ID: "999999@lid", Canonical: "12025550123@c.us"}}, nil, nil, nil)
	got, err := ProjectIncomingReactions("account", legacyIncomingReactionEvidence(), d)
	if err != nil || len(got.States) != 1 || len(got.Echoes) != 2 || len(got.Caches) != 3 {
		t.Fatal("incoming reaction facts lost", err)
	}
	state := got.States[0].State
	actor := state.Actors["12025550123@c.us"]
	if state.Full || len(state.Known) != 2 || !state.Known["888888@lid"] || state.Revision != 5 || actor.Order != 2 || actor.Echo == nil || actor.Echo.Target.ThreadID != 44 {
		t.Fatal("ordering, original echo or absence evidence changed")
	}
	var group, member, master bool
	for _, cache := range got.Caches {
		switch cache.DestinationKey {
		case "*:-100:44":
			group = cache.PendingFallback == "ow:reaction" && cache.Cache.Applied["master"].MessageID == 10 && cache.Cache.Applied["master"].Actor == "" && cache.Cache.Refused["77"].At == 1700000000123 && cache.Cache.Refused["77"].Reason == "message_missing"
		case "77:123:0":
			member = cache.ChatID == "12025550123@c.us" && cache.Cache.Applied["77"].Emoji == "👍" && len(cache.Cache.Applied) == 1
		case "master:123:0":
			master = cache.ChatID == "12025550123@c.us" && cache.Cache.Applied["master"].Emoji == "👎" && len(cache.Cache.Applied) == 1
		}
	}
	if !group || !member || !master {
		t.Fatal("private overlapping IDs, refusal timing or pending notice changed")
	}
	for _, echo := range got.Echoes {
		if echo.ChatID == "12025550123@c.us" && (echo.Echo.Target.BotID != "77" || echo.Echo.Target.ThreadID != 0) {
			t.Fatal("private echo borrowed the master copy")
		}
	}
}

func TestLegacyReactionEchoWithoutSurvivingCopyRequiresOriginalOwnerMainChatObservation(t *testing.T) {
	d, _ := identity.New(nil, nil, nil, nil)
	job := `{"bot":"master","update":{"message_reaction":{"chat":{"id":-100,"type":"supergroup"},"user":{"id":123,"is_bot":false},"message_id":10,"new_reaction":[{"type":"emoji","emoji":"👍"}]}}}`
	e := ReactionEvidence{OwnerID: 123, Bots: []string{"master"}, Jobs: []Job{{Type: "tg", State: "done", Payload: job}}, Values: []KV{
		{Kind: "wa-complete", ID: "12345@g.us:source", Value: "true"},
		{Kind: "reaction-echo", ID: reactionSourceKey("account", "12345@g.us", "source"), Value: `{"sender":"12025550999@c.us","bot":"master","chat":"-100","message":10,"emoji":"👍","at":1700000000000}`},
	}}
	r, err := ProjectIncomingReactions("account", e, d)
	if err != nil || len(r.Echoes) != 1 || r.Echoes[0].Echo.Target.ThreadID != 0 || r.Echoes[0].Echo.MessageID != 10 || len(r.Caches) != 0 {
		t.Fatal("retained owner reference lost or converted into applied settings", err)
	}
	for _, mutate := range []func(*ReactionEvidence){
		func(v *ReactionEvidence) { v.OwnerID = 999 },
		func(v *ReactionEvidence) { v.Jobs[0].State = "pending" },
		func(v *ReactionEvidence) {
			v.Jobs[0].Payload = strings.Replace(job, `"type":"supergroup"`, `"type":"supergroup","is_forum":true`, 1)
		},
		func(v *ReactionEvidence) { v.Jobs[0].Payload = strings.Replace(job, `"bot":"master"`, `"bot":"77"`, 1) },
		func(v *ReactionEvidence) {
			v.Jobs[0].Payload = strings.Replace(job, `"message_id":10`, `"message_id":11`, 1)
		},
		func(v *ReactionEvidence) { v.Values = v.Values[1:] },
	} {
		changed := e
		changed.Jobs = append([]Job(nil), e.Jobs...)
		changed.Values = append([]KV(nil), e.Values...)
		mutate(&changed)
		if _, err := ProjectIncomingReactions("account", changed, d); err == nil {
			t.Fatal("unproven reaction reference accepted")
		}
	}
}

func TestLegacyIncomingReactionsRejectMissingScopeAndForeignTargets(t *testing.T) {
	d, _ := identity.New([]identity.Alias{{ID: "999999@lid", Canonical: "12025550123@c.us"}}, nil, nil, nil)
	for _, mutate := range []func(*ReactionEvidence){
		func(e *ReactionEvidence) { e.Copies = e.Copies[1:] },
		func(e *ReactionEvidence) {
			e.Values[0].ID = reactionSourceKey("foreign-account", "12345@g.us", "source")
		},
		func(e *ReactionEvidence) { e.Values[2].Value = `{"applied":{"master":"[42,\"👍\"]"},"refused":{}}` },
		func(e *ReactionEvidence) { e.Values[3].Value = `{"applied":{"master":"[42,\"👍\"]"},"refused":{}}` },
		func(e *ReactionEvidence) {
			e.Jobs[0].Payload = `{"event":"message.reaction","data":{"chatId":"77777@g.us","messageId":"source"}}`
		},
		func(e *ReactionEvidence) {
			e.Values[1].Value = `{"sender":"12025550999@c.us","bot":"77","chat":"123","message":42,"emoji":"👍","at":1700000000000}`
		},
	} {
		e := legacyIncomingReactionEvidence()
		mutate(&e)
		if _, err := ProjectIncomingReactions("account", e, d); !errors.Is(err, ErrReactionEvidence) {
			t.Fatal("missing or foreign source/copy evidence accepted", err)
		}
	}
}

func TestLegacyIncomingReactionsCanLocateAnUnmappedSourceFromItsOriginalJob(t *testing.T) {
	d, _ := identity.New(nil, nil, nil, nil)
	e := ReactionEvidence{
		Jobs:   []Job{{Type: "wa", Dedup: "ow:unmapped", Payload: `{"event":"message.reaction","data":{"chatId":"12345@g.us","messageId":"unmapped"}}`}},
		Values: []KV{{Kind: "wa-reaction-state", ID: reactionSourceKey("account", "12345@g.us", "unmapped"), Value: `{"revision":1,"event":"ow:unmapped","senders":{"888888@lid":{"emoji":"👍","order":1}}}`}},
	}
	got, err := ProjectIncomingReactions("account", e, d)
	if err != nil || len(got.States) != 1 || len(got.Caches) != 0 || len(got.Echoes) != 0 {
		t.Fatal("original unmapped reaction lost or invented a destination", err)
	}
	for _, invalid := range []string{`{"revision":0,"senders":{"888888@lid":{"emoji":"👍","order":1}}}`, `{"revision":1,"senders":{"888888@lid":{"emoji":null,"order":1}}}`, `{"revision":9223372036854775807,"senders":{}}`} {
		e.Values[0].Value = invalid
		if _, err := ProjectIncomingReactions("account", e, d); err == nil {
			t.Fatal("malformed actor state accepted")
		}
	}
}

func TestLegacyIncomingReactionFactsRejectConflictingAliasCaches(t *testing.T) {
	d, _ := identity.New([]identity.Alias{{ID: "999999@lid", Canonical: "12025550123@c.us"}}, nil, nil, nil)
	e := ReactionEvidence{Values: []KV{
		{Kind: "wamessage", ID: "999999@lid:private", Value: `{}`},
		{Kind: "wa-reaction-state", ID: reactionSourceKey("account", "999999@lid", "private"), Value: `{"revision":1,"senders":{"888888@lid":{"emoji":"👍","order":1}}}`},
		{Kind: "wa-reaction-state", ID: reactionSourceKey("account", "12025550123@c.us", "private"), Value: `{"revision":2,"senders":{"888888@lid":{"emoji":"👎","order":2}}}`},
	}}
	if _, err := ProjectIncomingReactions("account", e, d); !errors.Is(err, ErrReactionEvidence) {
		t.Fatal("unordered alias caches silently selected a winner", err)
	}
	// The projector must retain the original value strings, not rewrite JSON.
	if !json.Valid([]byte(e.Values[1].Value)) || e.Values[0].ID != "999999@lid:private" {
		t.Fatal("source evidence mutated")
	}
}

func TestLegacyIncomingReactionActorAliasesUseTheirSharedRevisionOrder(t *testing.T) {
	d, _ := identity.New([]identity.Alias{{ID: "999999@lid", Canonical: "12025550123@c.us"}}, nil, nil, nil)
	e := ReactionEvidence{Values: []KV{
		{Kind: "wamessage", ID: "12345@g.us:source", Value: `{}`},
		{Kind: "wa-reaction-state", ID: reactionSourceKey("account", "12345@g.us", "source"), Value: `{"revision":5,"senders":{"999999@lid":{"emoji":"👍","order":2},"12025550123@c.us":{"emoji":"❤","order":5}}}`},
	}}
	for range 20 {
		state, err := ProjectIncomingReactions("account", e, d)
		if err != nil || len(state.States) != 1 || len(state.States[0].State.Actors) != 1 || state.States[0].State.Actors["12025550123@c.us"].Emoji != "❤" || state.States[0].State.Actors["12025550123@c.us"].Order != 5 {
			t.Fatal("actor alias choice ignored its proven shared ordering", err)
		}
	}
}
