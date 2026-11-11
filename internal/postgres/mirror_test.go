package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

func mirrorFixture(t *testing.T) *relayFixture {
	t.Helper()
	f := newRelayFixture(t, nil, true)
	f.topicID = 88
	return f
}
func mirrorEvent(t *testing.T, f *relayFixture, key, kind, chat string, target int64, previous string) int64 {
	t.Helper()
	event := control.MirrorEvent{ID: key, Type: kind}
	event.Data.ChatID = chat
	event.Data.TelegramChat = target
	event.Data.PreviousReturn = previous
	b, _ := json.Marshal(event)
	if _, err := f.store.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: key, Stream: key, Kind: "management", Payload: b}); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func mirrorRoute(t *testing.T, f *relayFixture, chat string) (int64, int64, string) {
	t.Helper()
	r, ok, err := f.store.ConversationRoute(t.Context(), "fixture-account", chat)
	if err != nil || !ok {
		t.Fatal(r, ok, err)
	}
	return r.TelegramChatID, r.ThreadID, string(r.Mode)
}
func dueMirror(t *testing.T, f *relayFixture, key string) {
	t.Helper()
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE dedup=$1", key); err != nil {
		t.Fatal(err)
	}
}

func TestManualMirrorLinkAdmitsWholeGroupRouteAndIndependentRetirement(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "group@g.us", -100, 44)
	if r := f.incoming(t, "old-copy", relay.Incoming{ChatID: "group@g.us", MessageID: "old", Body: "archive"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := operatorCommand(t, f, 2000, "master", -200, 999, "/link group@g.us extra ignored"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if chat, _, _ := mirrorRoute(t, f, "group@g.us"); chat != -100 {
		t.Fatal("link ran in the source command")
	}
	if r := f.dispatch(t, operatorKey("master", 2000), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if chat, thread, mode := mirrorRoute(t, f, "group@g.us"); chat != -200 || thread != 0 || mode != "group" {
		t.Fatal(chat, thread, mode)
	}
	if last := f.tgCalls[len(f.tgCalls)-1]; last.Chat != -200 || last.Thread != 0 || !strings.HasPrefix(last.Text, "Linked WhatsApp group group@g.us.") {
		t.Fatal(last)
	}
	if r := f.dispatch(t, "retire-inbox:"+operatorKey("master", 2000), "control-retire-inbox"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(f.retireCalls) != 1 || f.retireCalls[0].Thread != 44 || !rowDeleted(t, f.store, "*", -100, 1001) {
		t.Fatal(f.retireCalls)
	}
	var maps int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_mappings").Scan(&maps); err != nil || maps != 1 {
		t.Fatal(maps, err)
	}
	if r := f.incoming(t, "mirror-copy", relay.Incoming{ChatID: "group@g.us", MessageID: "new", Body: "mirror"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if last := f.tgCalls[len(f.tgCalls)-1]; last.Chat != -200 || last.Thread != 0 || f.reads.Load() != 0 {
		t.Fatal(last)
	}
}
func TestManualMirrorRefusesPublicDestinationsAndMissingAdminPermission(t *testing.T) {
	for _, kind := range []string{"public username", "active public username", "channel", "member", "creator", "already linked", "unknown conversation"} {
		t.Run(kind, func(t *testing.T) {
			f := mirrorFixture(t)
			f.route(t, "group@g.us", -100, 44)
			switch kind {
			case "public username":
				f.chatUsername = "public_group"
			case "active public username":
				f.chatActiveUsernames = []string{"public_group"}
			case "channel":
				f.chatType = "channel"
			case "member", "creator":
				f.masterRole = kind
			case "already linked":
				f.route(t, "other@g.us", -200, 0)
			}
			wa := "group@g.us"
			if kind == "unknown conversation" {
				wa = "unknown@g.us"
			}
			if r := operatorCommand(t, f, 2010, "master", -200, 0, "/link "+wa); r.State != delivery.Done {
				t.Fatal(r)
			}
			if r := f.dispatch(t, operatorKey("master", 2010), "management"); r.State != delivery.Failed {
				t.Fatal(r)
			}
			if chat, _, _ := mirrorRoute(t, f, "group@g.us"); chat != -100 || len(f.retireCalls) != 0 || len(f.tgCalls) != 0 {
				t.Fatal(chat, f.tgCalls)
			}
		})
	}
}

func TestManualMirrorConfirmationRecoveryNeverReappliesRouteChange(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "group@g.us", -100, 44)
	f.failTG = 1
	if r := operatorCommand(t, f, 2011, "master", -200, 0, "/link group@g.us"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 2011), "management"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	if err := f.store.PutIngressRoute(t.Context(), "fixture-account", ingress.Route{WhatsAppChat: "group@g.us", TelegramChatID: -300}); err != nil {
		t.Fatal(err)
	}
	dueMirror(t, f, operatorKey("master", 2011))
	if r := f.dispatch(t, operatorKey("master", 2011), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if chat, _, _ := mirrorRoute(t, f, "group@g.us"); chat != -300 || len(f.tgCalls) != 2 || f.tgCalls[0] != f.tgCalls[1] || f.meCalls.Load() != 1 {
		t.Fatal(chat, f.tgCalls, f.meCalls.Load())
	}
}

func TestManualMirrorRouteAndCleanupRollbackWithoutConfirmationPlan(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "group@g.us", -100, 44)
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans ADD CONSTRAINT fixture_refusal CHECK (plan_key NOT LIKE 'management:%')"); err != nil {
		t.Fatal(err)
	}
	if r := operatorCommand(t, f, 2012, "master", -200, 0, "/link group@g.us"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 2012), "management"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	if chat, _, _ := mirrorRoute(t, f, "group@g.us"); chat != -100 || len(f.tgCalls) != 0 {
		t.Fatal(chat, f.tgCalls)
	}
	var jobs int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='control-retire-inbox'").Scan(&jobs); err != nil || jobs != 0 {
		t.Fatal(jobs, err)
	}
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans DROP CONSTRAINT fixture_refusal"); err != nil {
		t.Fatal(err)
	}
	dueMirror(t, f, operatorKey("master", 2012))
	if r := f.dispatch(t, operatorKey("master", 2012), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
}
func TestUnmirrorCreatesFreshTopicPreservesArchivesAndStopsFormerMirror(t *testing.T) {
	f := mirrorFixture(t)
	activeMirror(t, f, "group@g.us")
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET name='Friends'"); err != nil {
		t.Fatal(err)
	}
	if r := f.incoming(t, "mirror-archive", relay.Incoming{ChatID: "group@g.us", MessageID: "old", Body: "archive"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := operatorCommand(t, f, 2020, "master", -200, 1001, "/unmirror"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if chat, _, _ := mirrorRoute(t, f, "group@g.us"); chat != -200 {
		t.Fatal(chat)
	}
	if r := f.dispatch(t, operatorKey("master", 2020), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if chat, thread, mode := mirrorRoute(t, f, "group@g.us"); chat != -100 || thread != 88 || mode != "inbox" {
		t.Fatal(chat, thread, mode)
	}
	if f.topics.Load() != 1 || f.topicNames[0] != "Friends" || len(f.retireCalls) != 0 || len(f.deleteCalls) != 0 || rowDeleted(t, f.store, "*", -200, 1001) {
		t.Fatal(f.topicNames, f.retireCalls)
	}
	last := f.tgCalls[len(f.tgCalls)-1]
	if last.Chat != -200 || last.Thread != 0 || !strings.Contains(last.Markup, "/88") {
		t.Fatal(last)
	}
	if r := f.outgoing(t, "old-mirror-reply", "master", ownerText(30000, -200, "former mirror")); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(f.waCalls) != 0 {
		t.Fatal("former mirror forwarded", f.waCalls)
	}
	if r := f.incoming(t, "after-return", relay.Incoming{ChatID: "group@g.us", MessageID: "new", Body: "new inbox", QuotedMessageID: "old"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	last = f.tgCalls[len(f.tgCalls)-1]
	if last.Chat != -100 || last.Thread != 88 || last.Reply != "" {
		t.Fatal(last)
	}
	m := ownerText(30001, -100, "current inbox")
	m.MessageThreadID = 88
	m.IsTopicMessage = true
	if r := f.outgoing(t, "new-topic-reply", "master", m); r.State != delivery.Done || len(f.waCalls) != 1 {
		t.Fatal(r, f.waCalls)
	}
	var header int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE dedup=$1", operatorKey("master", 2020)+":inbox").Scan(&header); err != nil || header != 1 {
		t.Fatal(header, err)
	}
}
func TestUnmirrorPreservesPausedPreferencesAndBlocksActiveImports(t *testing.T) {
	for _, state := range []string{"muted", "ignored", "queued", "fetching", "delivering"} {
		t.Run(state, func(t *testing.T) {
			f := mirrorFixture(t)
			activeMirror(t, f, "group@g.us")
			paused := state == "muted" || state == "ignored"
			if paused {
				if _, err := f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET mode=$1", state); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.store.pool.Exec(t.Context(), "INSERT INTO history_activity(account_id,whatsapp_chat,run_id,status) VALUES('fixture-account','group@g.us','run',$1)", state); err != nil {
					t.Fatal(err)
				}
			}
			if r := operatorCommand(t, f, 2030, "master", -200, 0, "/unmirror"); r.State != delivery.Done {
				t.Fatal(r)
			}
			if r := f.dispatch(t, operatorKey("master", 2030), "management"); r.State != delivery.Done {
				t.Fatal(r)
			}
			chat, _, mode := mirrorRoute(t, f, "group@g.us")
			last := f.tgCalls[len(f.tgCalls)-1]
			if paused {
				if chat != -100 || mode != state || !strings.Contains(last.Text, "Use /keep") {
					t.Fatal(chat, mode, last)
				}
			} else {
				if chat != -200 || f.topics.Load() != 0 || last.Text != control.UnmirrorBusy {
					t.Fatal(chat, last)
				}
			}
		})
	}
}
func TestUnmirrorDefiniteRefusalKeepsMirrorAndRetriesOnlyGuidance(t *testing.T) {
	for _, status := range []int{400, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := mirrorFixture(t)
			activeMirror(t, f, "group@g.us")
			f.topicStatus = status
			f.failTG = 1
			if r := operatorCommand(t, f, 2040, "master", -200, 0, "/unmirror"); r.State != delivery.Done {
				t.Fatal(r)
			}
			if r := f.dispatch(t, operatorKey("master", 2040), "management"); r.State != delivery.Pending {
				t.Fatal(r)
			}
			if chat, _, _ := mirrorRoute(t, f, "group@g.us"); chat != -200 {
				t.Fatal(chat)
			}
			dueMirror(t, f, operatorKey("master", 2040))
			if r := f.dispatch(t, operatorKey("master", 2040), "management"); r.State != delivery.Done {
				t.Fatal(r)
			}
			if f.topics.Load() != 1 || len(f.tgCalls) != 2 || f.tgCalls[0] != f.tgCalls[1] || f.tgCalls[1].Text != control.UnmirrorPermission {
				t.Fatal(f.tgCalls, f.topics.Load())
			}
			if err := f.store.PutIngressRoute(t.Context(), "fixture-account", ingress.Route{WhatsAppChat: "group@g.us", TelegramChatID: -300}); err != nil {
				t.Fatal("known refusal retained the move guard", err)
			}
		})
	}
}

func TestUnmirrorPreservesConfiguredCommandAndInteractiveLifetimes(t *testing.T) {
	f := mirrorFixture(t)
	activeMirror(t, f, "group@g.us")
	replies, err := control.New(f.store, f.replyClients, -100)
	if err != nil {
		t.Fatal(err)
	}
	f.mirrors, err = control.NewMirrors(f.store, replies, f.mirrorTelegram, control.MirrorOptions{OwnerID: 123, CommandTTL: 10 * time.Second, ControlTTL: 100 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if r := operatorCommand(t, f, 2045, "master", -200, 0, "/unmirror"); r.State != delivery.Done {
		t.Fatal(r)
	}
	var id int64
	if err := f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", operatorKey("master", 2045)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if r := f.run(t, id, "management", f.mirrors.Manage); r.State != delivery.Done {
		t.Fatal(r)
	}
	var command, notice int64
	if err := f.store.pool.QueryRow(t.Context(), "SELECT expires_at FROM telegram_observations WHERE kind='command'").Scan(&command); err != nil {
		t.Fatal(err)
	}
	if err := f.store.pool.QueryRow(t.Context(), "SELECT expires_at FROM telegram_observations WHERE kind='log'").Scan(&notice); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		deadline int64
		want     time.Duration
	}{{command, 10 * time.Second}, {notice, 100 * time.Second}} {
		if delta := time.Until(time.UnixMilli(tc.deadline)); delta < tc.want-5*time.Second || delta > tc.want+time.Second {
			t.Fatal(delta, tc.want)
		}
	}
}
func TestUnmirrorDiscardedTopicRefusalSurvivesLocalNoticeFailure(t *testing.T) {
	f := mirrorFixture(t)
	activeMirror(t, f, "group@g.us")
	f.topicStatus = 403
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans ADD CONSTRAINT fixture_refusal CHECK (plan_key NOT LIKE 'management:%')"); err != nil {
		t.Fatal(err)
	}
	if r := operatorCommand(t, f, 2050, "master", -200, 0, "/unmirror"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 2050), "management"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans DROP CONSTRAINT fixture_refusal"); err != nil {
		t.Fatal(err)
	}
	dueMirror(t, f, operatorKey("master", 2050))
	if r := f.dispatch(t, operatorKey("master", 2050), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if f.topics.Load() != 1 || len(f.tgCalls) != 1 || f.tgCalls[0].Text != control.UnmirrorPermission {
		t.Fatal(f.tgCalls, f.topics.Load())
	}
}
func TestUnmirrorLostTopicResponseRetainsUncertaintyAndRouteImportGuards(t *testing.T) {
	f := mirrorFixture(t)
	activeMirror(t, f, "group@g.us")
	f.dropTopicAt = 1
	if r := operatorCommand(t, f, 2060, "master", -200, 0, "/unmirror"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 2060), "management"); r.State != delivery.Uncertain {
		t.Fatal(r)
	}
	if err := f.store.PutIngressRoute(t.Context(), "fixture-account", ingress.Route{WhatsAppChat: "group@g.us", TelegramChatID: -300}); !errors.Is(err, delivery.ErrLocalRetry) {
		t.Fatal(err)
	}
	id := f.enqueue(t, "management", "begin-history", "history:group", "")
	if r := f.run(t, id, "management", func(ctx context.Context, j delivery.Job, _ *delivery.Effects) error {
		return f.store.RecordHistoryActivity(ctx, j, "group@g.us", "run", "queued")
	}); r.State != delivery.Pending {
		t.Fatal(r)
	}
	if chat, _, _ := mirrorRoute(t, f, "group@g.us"); chat != -200 || f.topics.Load() != 1 || len(f.tgCalls) != 0 {
		t.Fatal(chat, f.topics.Load(), f.tgCalls)
	}
}
func TestUnmirrorTopicReceiptSurvivesCommitFailureAndNewConsumer(t *testing.T) {
	f := mirrorFixture(t)
	activeMirror(t, f, "group@g.us")
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE bridge_routes ADD CONSTRAINT fixture_refusal CHECK (thread_id<>88)"); err != nil {
		t.Fatal(err)
	}
	if r := operatorCommand(t, f, 2070, "master", -200, 0, "/unmirror"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 2070), "management"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE bridge_routes DROP CONSTRAINT fixture_refusal"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), f.store.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	f.store.Close()
	f.store = reopened
	// Even making the former mirror the newly configured management chat
	// cannot invalidate recovery of the original frozen topic receipt.
	replies, err := control.New(reopened, f.replyClients, -200)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := control.NewMirrors(reopened, replies, f.mirrorTelegram, control.MirrorOptions{OwnerID: 456, ControlTTL: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	dueMirror(t, f, operatorKey("master", 2070))
	var id int64
	if err := reopened.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", operatorKey("master", 2070)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if r := f.run(t, id, "management", restarted.Manage); r.State != delivery.Done {
		t.Fatal(r)
	}
	if chat, thread, _ := mirrorRoute(t, f, "group@g.us"); chat != -100 || thread != 88 || f.topics.Load() != 1 || len(f.tgCalls) != 1 {
		t.Fatal(chat, thread, f.topicNames, f.tgCalls)
	}
}
func TestUnmirrorObsoleteNoticeAndABAReturnCannotUndoLaterLink(t *testing.T) {
	f := mirrorFixture(t)
	activeMirror(t, f, "group@g.us")
	f.failTG = 1
	if r := operatorCommand(t, f, 2080, "master", -200, 0, "/unmirror"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 2080), "management"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	// A later link to the same former group creates an ABA destination change.
	if r := operatorCommand(t, f, 2081, "master", -200, 0, "/link group@g.us"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 2081), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	notices := len(f.tgCalls)
	dueMirror(t, f, operatorKey("master", 2080))
	if r := f.dispatch(t, operatorKey("master", 2080), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if chat, _, _ := mirrorRoute(t, f, "group@g.us"); chat != -200 || len(f.tgCalls) != notices {
		t.Fatal(chat, f.tgCalls)
	}
	key := "old-return"
	mirrorEvent(t, f, key, "return_to_inbox", "group@g.us", -200, "")
	if r := f.dispatch(t, key, "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if f.topics.Load() != 1 || f.tgCalls[len(f.tgCalls)-1].Text != control.UnmirrorStale {
		t.Fatal(f.topicNames, f.tgCalls)
	}
}
func TestMirrorDestinationCommitsAreSerializedAcrossConversations(t *testing.T) {
	f := mirrorFixture(t)
	f.route(t, "one@g.us", -100, 44)
	f.route(t, "two@g.us", -100, 45)
	mirrorEvent(t, f, "link-one", "link_group", "one@g.us", -200, "")
	mirrorEvent(t, f, "link-two", "link_group", "two@g.us", -200, "")
	a, b := claim(t, f.store), claim(t, f.store)
	for _, j := range []delivery.Job{a, b} {
		if _, err := f.store.PrepareTransition(t.Context(), j, -100, 123, 60, 600); err != nil {
			t.Fatal(err)
		}
	}
	results := make(chan error, 2)
	for _, j := range []delivery.Job{a, b} {
		go func() { results <- f.store.CommitMirrorLink(t.Context(), j) }()
	}
	x, y := <-results, <-results
	if !((x == nil && errors.Is(y, delivery.ErrInvalid)) || (y == nil && errors.Is(x, delivery.ErrInvalid))) {
		t.Fatal(x, y)
	}
	var n int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM bridge_routes WHERE telegram_chat_id=-200").Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
func TestMirrorSourceAdmissionRollbackAuthorizationAndEdits(t *testing.T) {
	f := mirrorFixture(t)
	activeMirror(t, f, "group@g.us")
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans ADD CONSTRAINT fixture_refusal CHECK (payload->'Mirror'='null'::jsonb)"); err != nil {
		t.Fatal(err)
	}
	if r := operatorCommand(t, f, 2090, "master", -200, 0, "/unmirror"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	var n int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE dedup=$1", operatorKey("master", 2090)).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans DROP CONSTRAINT fixture_refusal"); err != nil {
		t.Fatal(err)
	}
	// Separate source stream checks cannot execute an edited command.
	u := models.Update{ID: 2091, EditedMessage: pickerMessage(9, -300, 0, "/unmirror")}
	if !admitPicker(t, f, "master", u) {
		t.Fatal("edit not admitted")
	}
	if r := f.dispatch(t, "tg:master:2091", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	u = models.Update{ID: 2092, Message: pickerMessage(10, -200, 0, "/unmirror")}
	u.Message.From.ID = 999
	if admitPicker(t, f, "master", u) {
		t.Fatal("unauthorized actor admitted")
	}
	if f.topics.Load() != 0 || len(f.tgCalls) != 0 {
		t.Fatal(f.topicNames, f.tgCalls)
	}
}
