package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/bridge"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/reactions"
)

type reactionFixture struct {
	f *changeFixture
	s *reactions.Service
}

func newReactionFixture(t *testing.T) *reactionFixture {
	t.Helper()
	f := newChangeFixture(t)
	service, err := reactions.New(f.n.f.store, f.n.f.routing, func(_ context.Context, _ string, id string) (reactions.Telegram, error) {
		return f.n.tgClients[id], nil
	}, f.n.waClient, reactions.Settings{Session: "fixture-session", OwnerID: 123, NoticeTTL: 90})
	if err != nil {
		t.Fatal(err)
	}
	options := f.n.f.dispatchOptions
	options.Extensions.WhatsApp = map[sdk.WebhookEvent]delivery.Handler{sdk.EventMessageReceived: f.n.native.Incoming, sdk.EventMessageSent: f.n.native.Incoming, sdk.EventMessageEdited: f.edits.WhatsApp, sdk.EventMessageReaction: service.Incoming}
	options.Extensions.Telegram = func(ctx context.Context, j delivery.Job, e *delivery.Effects, id string, u *models.Update) error {
		for _, h := range []bridge.TelegramHandler{service.Outgoing, f.edits.Telegram, f.n.outbound.Handle} {
			if err := h(ctx, j, e, id, u); !errors.Is(err, delivery.ErrUnsupported) {
				return err
			}
		}
		return delivery.ErrUnsupported
	}
	if options.Extensions.ControlJobs == nil {
		options.Extensions.ControlJobs = map[string]delivery.Handler{}
	}
	options.Extensions.ControlJobs["control-notice"] = f.replies.Notice
	f.n.f.dispatcher, err = bridge.New(f.n.f.store, f.n.f.service, f.n.f.whatsapp, options)
	if err != nil {
		t.Fatal(err)
	}
	return &reactionFixture{f: f, s: service}
}
func (f *reactionFixture) source(t *testing.T, key, actor, emoji string, snapshot map[string]string) delivery.Report {
	t.Helper()
	data := map[string]any{"chatId": "peer@g.us", "messageId": "source-image", "senderId": actor, "reaction": emoji}
	if snapshot != nil {
		data["reactions"] = snapshot
	}
	raw, _ := json.Marshal(map[string]any{"event": "message.reaction", "sessionId": "fixture-session", "idempotencyKey": key, "data": data})
	if !fixtureIngress(t, f.f.n.f)("openwa", raw) {
		t.Fatal("reaction not admitted")
	}
	return f.f.n.f.dispatch(t, "ow:"+key, "wa")
}
func standardReaction(emoji string) models.ReactionType {
	return models.ReactionType{Type: models.ReactionTypeTypeEmoji, ReactionTypeEmoji: &models.ReactionTypeEmoji{Emoji: emoji}}
}
func customReaction(id string) models.ReactionType {
	return models.ReactionType{Type: models.ReactionTypeTypeCustomEmoji, ReactionTypeCustomEmoji: &models.ReactionTypeCustomEmoji{CustomEmojiID: id}}
}
func (f *reactionFixture) outgoing(t *testing.T, id int64, old, next []models.ReactionType) delivery.Report {
	t.Helper()
	u := models.Update{ID: id, MessageReaction: &models.MessageReactionUpdated{Chat: models.Chat{ID: -100, Type: models.ChatTypeSupergroup}, MessageID: 2001, User: &models.User{ID: 123}, Date: 1700000000, OldReaction: old, NewReaction: next}}
	if !admitPicker(t, f.f.n.f, "master", u) {
		t.Fatal("owner reaction not admitted")
	}
	return f.f.n.f.dispatch(t, "tg:master:"+strconv.FormatInt(id, 10), "tg")
}
func reactionEmoji(t *testing.T, call nativeCall) string {
	t.Helper()
	if call.Reaction == "" {
		return ""
	}
	var selected []models.ReactionType
	if err := json.Unmarshal([]byte(call.Reaction), &selected); err != nil {
		t.Fatal(err)
	}
	if len(selected) == 0 {
		return ""
	}
	if selected[0].ReactionTypeEmoji == nil {
		t.Fatal(selected)
	}
	return selected[0].ReactionTypeEmoji.Emoji
}

func TestReactionSnapshotClearsOnlyDepartingMemberBot(t *testing.T) {
	f := newReactionFixture(t)
	f.f.n.f.bind(t, "200", "12025550123@c.us")
	f.f.n.f.bind(t, "201", "12025550124@c.us")
	if r := f.f.n.incoming(t, "source-image", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	for _, event := range []struct {
		key, actor, emoji string
		all               map[string]string
	}{{"a", "12025550123@c.us", "👍", nil}, {"b", "12025550124@c.us", "❤", nil}, {"snapshot", "12025550123@c.us", "", map[string]string{"12025550124@c.us": "❤"}}} {
		if r := f.source(t, event.key, event.actor, event.emoji, event.all); r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	if len(f.f.n.calls) != 4 {
		t.Fatal(f.f.n.calls)
	}
	want := []struct{ bot, emoji string }{{"200", "👍"}, {"201", "❤"}, {"200", ""}}
	for i, expected := range want {
		call := f.f.n.calls[i+1]
		if call.Method != "setMessageReaction" || call.Bot != expected.bot || reactionEmoji(t, call) != expected.emoji || call.MessageID != 2001 {
			t.Fatal(i, call)
		}
	}
	var effects int
	if err := f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM effects e JOIN jobs j ON j.id=e.job_id WHERE j.dedup IN ('ow:a','ow:b','ow:snapshot')").Scan(&effects); err != nil || effects != 0 {
		t.Fatal("native setter used ambiguous-send effects", effects, err)
	}
}
func TestReactionMemberMissingMessageReroutesAndCachesSignature(t *testing.T) {
	f := newReactionFixture(t)
	f.f.n.f.bind(t, "200", "12025550123@c.us")
	if r := f.f.n.incoming(t, "source-image", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.failMethod = "setMessageReaction"
	f.f.n.failBot = "200"
	f.f.n.failCode = 400
	f.f.n.failDescription = "Bad Request: message to react not found"
	if r := f.source(t, "member-missing", "12025550123@c.us", "😂", nil); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.source(t, "member-missing-again", "12025550123@c.us", "😂", nil); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	if len(f.f.n.calls) != 3 || f.f.n.calls[1].Bot != "200" || f.f.n.calls[2].Bot != "master" || reactionEmoji(t, f.f.n.calls[2]) != "🤣" {
		t.Fatal(f.f.n.calls)
	}
}
func TestReactionUnsupportedNativeUpdateQueuesAttributedIndependentNotice(t *testing.T) {
	f := newReactionFixture(t)
	if r := f.f.n.incoming(t, "source-image", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.failMethod = "setMessageReaction"
	f.f.n.failCode = 400
	if r := f.source(t, "unsupported-native", "12025550123@c.us", "👍", nil); r.State != delivery.Done {
		t.Fatal(r)
	}
	var id int64
	var raw []byte
	if err := f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT id,payload FROM jobs WHERE kind='control-notice'").Scan(&id, &raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "native reaction unavailable") {
		t.Fatal("attribution missing")
	}
	if r := f.f.n.f.run(t, id, "control-notice", f.f.replies.Notice); r.State != delivery.Done {
		t.Fatal(r)
	}
}
func TestReactionKnownRetryAndCrashReuseFrozenMemberSetter(t *testing.T) {
	f := newReactionFixture(t)
	f.f.n.f.bind(t, "200", "12025550123@c.us")
	if r := f.f.n.incoming(t, "source-image", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.dropMethod = "setMessageReaction"
	first := f.source(t, "reaction-crash", "12025550123@c.us", "👍", nil)
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	f.f.n.mu.Lock()
	f.f.n.dropMethod = ""
	f.f.n.mu.Unlock()
	f.f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", first.JobID)
	j := claim(t, f.f.n.f.store)
	if j.ID != first.JobID {
		t.Fatal(j)
	}
	p, found, err := f.f.n.f.store.ReactionPlan(t.Context(), "fixture-account", "ow:reaction-crash")
	if err != nil || !found || len(p.Steps) != 1 {
		t.Fatal(p, found, err)
	}
	step := p.Steps[0]
	if err := f.f.n.tgClients[step.BotID].SetReaction(t.Context(), &bot.SetMessageReactionParams{ChatID: step.Target.ChatID, MessageID: int(step.MessageID), Reaction: []models.ReactionType{standardReaction(step.Emoji)}}); err != nil {
		t.Fatal(err)
	}
	f.f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", j.ID)
	if count, err := f.f.n.f.store.RecoverExpired(t.Context()); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if r := f.f.n.f.dispatch(t, "ow:reaction-crash", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	if len(f.f.n.calls) != 4 || f.f.n.calls[1].Bot != f.f.n.calls[2].Bot || f.f.n.calls[2].Reaction != f.f.n.calls[3].Reaction {
		t.Fatal("native setter changed across recovery", f.f.n.calls)
	}
}
func TestReactionUnresolvedCustomEmojiDoesNotClearPriorSourceReaction(t *testing.T) {
	f := newReactionFixture(t)
	if r := f.f.n.incoming(t, "source-image", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.outgoing(t, 101, nil, []models.ReactionType{standardReaction("👍")}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.outgoing(t, 102, []models.ReactionType{standardReaction("👍")}, []models.ReactionType{customReaction("12345")}); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	if len(f.f.n.sends) != 1 || string(f.f.n.sends[0].Request["emoji"]) != `"👍"` {
		t.Fatal("unresolved emoji cleared source", f.f.n.sends)
	}
	var notices int
	f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='control-notice'").Scan(&notices)
	if notices != 1 {
		t.Fatal(notices)
	}
}
func TestReactionCustomEquivalentFreezesAcrossRetryAndSuppressesEcho(t *testing.T) {
	f := newReactionFixture(t)
	if r := f.f.n.incoming(t, "source-image", "Caption"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if _, err := f.f.n.f.store.pool.Exec(t.Context(), "INSERT INTO whatsapp_self(account_id,person_id) VALUES('fixture-account','12025550123@c.us')"); err != nil {
		t.Fatal(err)
	}
	f.f.n.customEmoji = "❤️"
	f.f.n.sendFailure = 429
	f.f.n.sendFailMethod = "react"
	first := f.outgoing(t, 101, nil, []models.ReactionType{customReaction("12345")})
	if first.State != delivery.Pending {
		t.Fatal(first)
	}
	f.f.n.mu.Lock()
	f.f.n.customEmoji = "👎"
	f.f.n.sendFailure = 0
	f.f.n.mu.Unlock()
	f.f.n.f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE id=$1", first.JobID)
	if r := f.f.n.f.dispatch(t, "tg:master:101", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.outgoing(t, 102, []models.ReactionType{customReaction("12345")}, []models.ReactionType{standardReaction("❤")}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.source(t, "reaction-echo", "12025550123@c.us", "❤️", nil); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.mu.Lock()
	defer f.f.n.mu.Unlock()
	if len(f.f.n.sends) != 2 || string(f.f.n.sends[0].Request["emoji"]) != `"❤️"` || string(f.f.n.sends[1].Request["emoji"]) != `"❤️"` {
		t.Fatal("custom emoji refetched or duplicate equivalence resent", f.f.n.sends)
	}
	for _, call := range f.f.n.calls {
		if call.Method == "setMessageReaction" {
			t.Fatal("source owner echo was rendered", call)
		}
	}
}

func TestReactionAttachmentIsPrimaryForSplitSourceAndMissingTargetStaysPassive(t *testing.T) {
	f := newReactionFixture(t)
	if r := f.f.n.incoming(t, "source-image", strings.Repeat("caption", 200)); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.source(t, "attachment-reaction", "12025550123@c.us", "👍", nil); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.f.n.mu.Lock()
	if len(f.f.n.calls) != 3 || f.f.n.calls[2].MessageID != 2002 {
		t.Fatal("reaction targeted split label instead of attachment", f.f.n.calls)
	}
	f.f.n.failMethod = "setMessageReaction"
	f.f.n.failCode = 400
	f.f.n.failDescription = "Bad Request: message to react not found"
	f.f.n.mu.Unlock()
	if r := f.source(t, "missing-reaction-target", "12025550123@c.us", "❤", nil); r.State != delivery.Done {
		t.Fatal(r)
	}
	var notices int
	f.f.n.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='control-notice'").Scan(&notices)
	if notices != 0 {
		t.Fatal("missing reaction target replaced", notices)
	}
	if r := f.f.n.incoming(t, "next-source", "Next"); r.State != delivery.Done {
		t.Fatal("missing reaction target blocked next message", r)
	}
}

func TestReactionPrivateOverlappingMessageIDsUseOriginalBotScope(t *testing.T) {
	f := newReactionFixture(t)
	f.f.n.perBotNativeIDs = true
	contacts := []string{"12025550123@c.us", "12025550124@c.us"}
	for i, contact := range contacts {
		id := strconv.Itoa(200 + i)
		f.f.n.f.bind(t, id, contact)
		message := sdk.ChatHistoryMessage{ID: "private-source", ChatID: contact, Body: "Private", Type: sdk.MsgImage, Timestamp: 1700000000, Media: &sdk.ChatHistoryMedia{Mimetype: "image/png"}}
		raw, _ := json.Marshal(map[string]any{"event": "message.received", "sessionId": "fixture-session", "idempotencyKey": "private-source-" + id, "data": message})
		if !fixtureIngress(t, f.f.n.f)("openwa", raw) {
			t.Fatal("private source not admitted")
		}
		if r := f.f.n.f.dispatch(t, "ow:private-source-"+id, "wa"); r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	for i, contact := range contacts {
		id := strconv.Itoa(200 + i)
		update := models.Update{ID: 101, MessageReaction: &models.MessageReactionUpdated{Chat: models.Chat{ID: 123, Type: models.ChatTypePrivate}, MessageID: 2001, User: &models.User{ID: 123}, Date: 1700000000, NewReaction: []models.ReactionType{standardReaction("👍")}}}
		if !admitPicker(t, f.f.n.f, id, update) {
			t.Fatal("private reaction not admitted")
		}
		if r := f.f.n.f.dispatch(t, "tg:"+id+":101", "tg"); r.State != delivery.Done {
			t.Fatal(r)
		}
		f.f.n.mu.Lock()
		if len(f.f.n.sends) != i+1 || string(f.f.n.sends[i].Request["chatId"]) != strconv.Quote(contact) {
			t.Fatal("private IDs leaked across bots", f.f.n.sends)
		}
		f.f.n.mu.Unlock()
	}
}
