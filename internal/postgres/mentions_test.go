package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

const mentionAlice = "12025550123@c.us"
const mentionBob = "12025550124@c.us"
const mentionGroup = "123@g.us"

func prepareMentions(t *testing.T, f *relayFixture) {
	t.Helper()
	f.route(t, mentionGroup, -200, 0)
	f.bind(t, "200", mentionAlice)
	if err := f.store.PutIngressBot(t.Context(), "fixture-account", ingress.Bot{ID: "200", Username: "alice_bot"}); err != nil {
		t.Fatal(err)
	}
	id := f.enqueue(t, "management", "mention-profiles", "mention-profiles", "")
	r := f.run(t, id, "management", func(ctx context.Context, j delivery.Job, _ *delivery.Effects) error {
		if err := f.store.SaveContactIdentity(ctx, j, "", "", identity.Profile{ID: mentionAlice, Name: "Alice"}); err != nil {
			return err
		}
		return f.store.SaveContactIdentity(ctx, j, "", "", identity.Profile{ID: mentionBob, Name: "Bob"})
	})
	if r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	f.groupRows = []sdk.GroupParticipant{{ID: mentionAlice, Name: "Alice"}, {ID: mentionBob, Name: "Bob"}}
	f.mu.Unlock()
}
func TestIncomingMentionsFreezeNativeRenderingBeforeRateRetry(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	prepareMentions(t, f)
	f.failTG = 1
	m := relay.Incoming{ChatID: mentionGroup, MessageID: "tagged", SenderID: mentionBob, Name: "Bob", Body: "😀 Hi @12025550123", MentionedIDs: []string{mentionAlice}}
	r := f.incoming(t, "ow:tagged", m)
	if r.State != delivery.Pending || r.Delay != time.Minute {
		t.Fatal(r)
	}
	f.mu.Lock()
	first := f.tgCalls[0]
	f.mu.Unlock()
	var entities []models.MessageEntity
	if err := json.Unmarshal([]byte(first.Entities), &entities); err != nil {
		t.Fatal(err)
	}
	if first.Text != "Bob: 😀 Hi @Alice" || len(entities) != 3 || entities[2].Type != models.MessageEntityTypeTextMention || entities[2].User.ID != 200 || entities[2].Offset != 11 {
		t.Fatal(first, entities)
	}
	f.memberLeft.Store(true)
	if err := f.store.PutIngressBot(t.Context(), "fixture-account", ingress.Bot{ID: "200", Username: "renamed_bot"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	r = f.run(t, r.JobID, "wa", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		return f.service.IncomingText(ctx, j, e, m)
	})
	if r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tgCalls) != 2 || f.tgCalls[1] != first || f.memberCalls.Load() != 1 || f.groupCalls.Load() != 0 {
		t.Fatal("rendering or membership lookup changed on retry", f.tgCalls)
	}
}
func TestOutgoingMentionsFreezeNativeTagsAndKeyAcrossBindingAndRosterChanges(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	prepareMentions(t, f)
	f.failWA = 1
	m := ownerText(71, -200, "Hello @ALICE_BOT and @unknown")
	r := f.outgoing(t, "tg:tagged", "master", m)
	if r.State != delivery.Pending || r.Delay != time.Minute {
		t.Fatal(r)
	}
	f.mu.Lock()
	first := f.waCalls[0]
	f.mu.Unlock()
	if first.Text != "Hello @12025550123 and @unknown" || !reflect.DeepEqual(first.Mentions, []string{mentionAlice}) {
		t.Fatal(first)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE bridge_contact_bots SET contact_id=$1 WHERE bot_id='200'", mentionBob); err != nil {
		t.Fatal(err)
	}
	f.groupFailure.Store(true)
	m.Text = "Changed @alice_bot"
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", r.JobID); err != nil {
		t.Fatal(err)
	}
	r = f.run(t, r.JobID, "tg", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		return f.service.OutgoingText(ctx, j, e, relay.Outgoing{BotID: "master", Message: m})
	})
	if r.State != delivery.Done {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.waCalls) != 2 || !reflect.DeepEqual(f.waCalls[1], first) || f.groupCalls.Load() != 1 {
		t.Fatal("resolved tags, key or roster changed during retry", f.waCalls)
	}
}
func TestUnavailableRosterRefusesMentionsWithoutSendingOrMarkingRead(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	prepareMentions(t, f)
	f.groupFailure.Store(true)
	r := f.outgoing(t, "tg:no-roster", "master", ownerText(72, -200, "@alice_bot"))
	if r.State != delivery.Failed {
		t.Fatal(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.waCalls) != 0 || f.reads.Load() != 0 {
		t.Fatal("unvalidated tag reached provider")
	}
}
