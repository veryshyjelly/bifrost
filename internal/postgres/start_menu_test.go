package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

func bindBeforeStart(t *testing.T, f *relayFixture, contact string) {
	t.Helper()
	if err := f.store.PutIngressBot(t.Context(), "fixture-account", ingress.Bot{ID: "200", Username: "alice_bot"}); err != nil {
		t.Fatal(err)
	}
	id := f.enqueue(t, "tg", "bind-before-start", "binding:200", "master")
	if r := f.run(t, id, "tg", func(ctx context.Context, j delivery.Job, _ *delivery.Effects) error {
		return f.store.BindContactBot(ctx, j, contact, "200")
	}); r.State != delivery.Done {
		t.Fatal(r)
	}
}
func TestStartActivatesBoundBotAndCreatesMissingDirectRouteWithoutTopic(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	bindBeforeStart(t, f, "12025550100@c.us")
	if r := operatorCommand(t, f, 1100, "200", 123, 0, "/start bridge"); r.State != delivery.Done {
		t.Fatal(r)
	}
	r, found, err := f.store.ConversationRoute(t.Context(), "fixture-account", "12025550100@c.us")
	if err != nil || !found || r.Mode != "dedicated" || !r.InboxRetired || r.ThreadID != 0 || f.topics.Load() != 0 {
		t.Fatal(r, found, err)
	}
	if len(f.tgCalls) != 1 || f.tgCalls[0].Text != control.MemberReady || f.tgCalls[0].Bot != "child-200" || f.tgCalls[0].Chat != 123 {
		t.Fatal(f.tgCalls)
	}
	var cleanup int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='control-retire-inbox'").Scan(&cleanup); err != nil || cleanup != 1 {
		t.Fatal(cleanup, err)
	}
	if r := f.incoming(t, "after-start", relay.Incoming{ChatID: "12025550100@c.us", SenderID: "12025550100@c.us", MessageID: "hello", Body: "private"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if last := f.tgCalls[len(f.tgCalls)-1]; last.Chat != 123 || last.Bot != "child-200" || f.reads.Load() != 0 {
		t.Fatal(last)
	}
}
func TestStartKeepsExistingInboxUntilIndependentRetirement(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.route(t, "12025550100@c.us", -100, 44)
	bindBeforeStart(t, f, "12025550100@c.us")
	if r := operatorCommand(t, f, 1110, "200", 123, 0, "/start"); r.State != delivery.Done {
		t.Fatal(r)
	}
	r, _, err := f.store.ConversationRoute(t.Context(), "fixture-account", "12025550100@c.us")
	if err != nil || r.ThreadID != 44 || r.InboxRetired {
		t.Fatal(r, err)
	}
	if r := f.dispatch(t, "retire-inbox:tg:200:1110", "control-retire-inbox"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(f.retireCalls) != 1 || f.retireCalls[0].Thread != 44 {
		t.Fatal(f.retireCalls)
	}
}
func TestStartUnboundActivationAndReplyRetryStayFrozen(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	if err := f.store.PutIngressBot(t.Context(), "fixture-account", ingress.Bot{ID: "200", Username: "alice_bot"}); err != nil {
		t.Fatal(err)
	}
	f.failTG = 1
	if r := operatorCommand(t, f, 1120, "200", 123, 0, "/start"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	var started bool
	if err := f.store.pool.QueryRow(t.Context(), "SELECT started FROM bridge_bots WHERE bot_id='200'").Scan(&started); err != nil || !started {
		t.Fatal(started, err)
	}
	// A binding added after preparation cannot change the saved help reply or
	// retroactively admit a direct route/retirement from the older Start.
	id := f.enqueue(t, "tg", "later-bind", "binding:200", "master")
	if r := f.run(t, id, "tg", func(ctx context.Context, j delivery.Job, _ *delivery.Effects) error {
		return f.store.BindContactBot(ctx, j, "12025550100@c.us", "200")
	}); r.State != delivery.Done {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE dedup='tg:200:1120'"); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, "tg:200:1120", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(f.tgCalls) != 2 || f.tgCalls[0] != f.tgCalls[1] || f.tgCalls[1].Text != control.MemberUnbound {
		t.Fatal(f.tgCalls)
	}
	var routes, retire int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM bridge_routes").Scan(&routes); err != nil {
		t.Fatal(err)
	}
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='control-retire-inbox'").Scan(&retire); err != nil || routes != 0 || retire != 0 {
		t.Fatal(routes, retire, err)
	}
	if n, err := f.store.QueueMenus(t.Context(), "fixture-account", "not-migrated"); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
func TestStartActivationAndHelpPlanRollbackTogether(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	bindBeforeStart(t, f, "12025550100@c.us")
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans ADD CONSTRAINT fixture_refusal CHECK (payload->'Start'='null'::jsonb)"); err != nil {
		t.Fatal(err)
	}
	if r := operatorCommand(t, f, 1130, "200", 123, 0, "/start"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	var started bool
	var n int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT started FROM bridge_bots WHERE bot_id='200'").Scan(&started); err != nil || started {
		t.Fatal(started, err)
	}
	if err := f.store.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM bridge_routes)+(SELECT count(*) FROM jobs WHERE kind='control-retire-inbox')").Scan(&n); err != nil || n != 0 || len(f.tgCalls) != 0 {
		t.Fatal(n, err)
	}
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans DROP CONSTRAINT fixture_refusal; UPDATE jobs SET due=clock_timestamp() WHERE state='pending'"); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, "tg:200:1130", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
}
func TestStartHelpAddressingScopesAndLifetime(t *testing.T) {
	for _, tc := range []struct {
		name, bot, text, want string
		chat, thread          int64
		button                bool
	}{
		{"master DM", "master", "/start", control.MasterHelp, 123, 0, false},
		{"master group", "master", "/start", control.MasterGroupHelp, -100, 44, false},
		{"directed member", "200", "/start@ALICE_bot", control.MemberGroupHelp, -200, 999, true},
		{"foreign recipient", "master", "/start@alice_bot", "", -200, 0, false},
		{"unqualified member", "200", "/start", "", -200, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRelayFixture(t, nil, true)
			f.route(t, "peer@g.us", -200, 0)
			if err := f.store.PutIngressBot(t.Context(), "fixture-account", ingress.Bot{ID: "200", Username: "alice_bot"}); err != nil {
				t.Fatal(err)
			}
			u := models.Update{ID: 1140, Message: pickerMessage(11140, tc.chat, tc.thread, tc.text)}
			if admitPicker(t, f, tc.bot, u) {
				if r := f.dispatch(t, "tg:"+tc.bot+":1140", "tg"); r.State != delivery.Done {
					t.Fatal(r)
				}
			}
			if tc.want == "" {
				if len(f.tgCalls) != 0 {
					t.Fatal(f.tgCalls)
				}
				return
			}
			if len(f.tgCalls) != 1 || f.tgCalls[0].Text != tc.want {
				t.Fatal(f.tgCalls)
			}
			if tc.button {
				if f.tgCalls[0].Markup != `{"inline_keyboard":[[{"text":"Open private chat","url":"https://t.me/alice_bot?start=bridge"}]]}` || f.tgCalls[0].Thread != 0 {
					t.Fatal(f.tgCalls[0])
				}
			}
			var ttl int64
			if err := f.store.pool.QueryRow(t.Context(), "SELECT expires_at FROM telegram_observations WHERE kind='log'").Scan(&ttl); err != nil {
				t.Fatal(err)
			}
			want := time.Minute
			if tc.button {
				want = 10 * time.Minute
			}
			if delta := time.Until(time.UnixMilli(ttl)); delta < want-5*time.Second || delta > want+time.Second {
				t.Fatal(delta, want)
			}
		})
	}
}
func TestStartDeepLinksEditsAndUnauthorizedActorsDoNotActivate(t *testing.T) {
	for _, kind := range []string{"mirror", "sync", "edited", "wrong owner", "bot actor"} {
		t.Run(kind, func(t *testing.T) {
			f := newRelayFixture(t, nil, true)
			if err := f.store.PutIngressBot(t.Context(), "fixture-account", ingress.Bot{ID: "200", Username: "alice_bot"}); err != nil {
				t.Fatal(err)
			}
			bot := "200"
			u := models.Update{ID: 1150, Message: pickerMessage(11150, 123, 0, "/start")}
			switch kind {
			case "mirror":
				bot = "master"
				u.Message.Text = "/start mirror_ab-cd"
			case "sync":
				bot = "master"
				u.Message = pickerMessage(11150, -200, 0, "/start sync_request")
			case "edited":
				u.EditedMessage = u.Message
				u.Message = nil
			case "wrong owner":
				u.Message.From.ID = 999
			case "bot actor":
				u.Message.From.IsBot = true
			}
			if admitPicker(t, f, bot, u) {
				r := f.dispatch(t, "tg:"+bot+":1150", "tg")
				if r.State != delivery.Done {
					t.Fatal(r)
				}
			}
			var started bool
			want := 0
			if kind == "mirror" {
				want = 1
				if f.tgCalls[0].Text != control.GroupButtonExpired {
					t.Fatal(f.tgCalls)
				}
			}
			if kind == "sync" {
				want = 1
				if f.tgCalls[0].Text != control.SyncPermissionExpired {
					t.Fatal(f.tgCalls)
				}
			}
			if err := f.store.pool.QueryRow(t.Context(), "SELECT started FROM bridge_bots WHERE bot_id='200'").Scan(&started); err != nil || started || len(f.tgCalls) != want {
				t.Fatal(started, f.tgCalls, err)
			}
		})
	}
}

func migratedMenuBot(t *testing.T, f *relayFixture, bot string, nativeID int64) {
	t.Helper()
	if bot != "master" {
		if err := f.store.PutIngressBot(t.Context(), "fixture-account", ingress.Bot{ID: bot}); err != nil {
			t.Fatal(err)
		}
	}
	id := f.enqueue(t, "management", "migrate:"+bot, "migrate:"+bot, bot)
	if r := f.run(t, id, "management", func(ctx context.Context, j delivery.Job, _ *delivery.Effects) error {
		return f.store.RecordBotMigration(ctx, j, bot, models.User{ID: nativeID, IsBot: true, Username: "prior_bot"})
	}); r.State != delivery.Done {
		t.Fatal(r)
	}
}
func TestMenuStartupSelectsOnlyMigratedBotsAndInstallsLegacyScopes(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	migratedMenuBot(t, f, "master", 999)
	migratedMenuBot(t, f, "200", 200)
	if err := f.store.PutIngressBot(t.Context(), "fixture-account", ingress.Bot{ID: "201"}); err != nil {
		t.Fatal(err)
	}
	if n, err := f.store.QueueMenus(t.Context(), "fixture-account", "boot"); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if n, err := f.store.QueueMenus(t.Context(), "fixture-account", "boot"); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	for _, bot := range []string{"200", "master"} {
		if r := f.dispatch(t, "control-menu:boot:"+bot, "control-menu"); r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	if len(f.menuCalls) != 5 {
		t.Fatal(f.menuCalls)
	}
	for i, c := range f.menuCalls {
		if c.Method == "setChatMenuButton" {
			if c.Button != `{"type":"commands"}` {
				t.Fatal(c)
			}
			continue
		}
		var got []models.BotCommand
		if err := json.Unmarshal([]byte(c.Commands), &got); err != nil {
			t.Fatal(err)
		}
		master := c.Bot == "master-fixture"
		scoped := i == 4
		if !reflect.DeepEqual(got, control.Commands(master, scoped)) {
			t.Fatal("wrong native command list", c)
		}
		want := `{"type":"default"}`
		if scoped {
			want = `{"type":"chat_member","chat_id":-100,"user_id":123}`
		}
		if c.Scope != want {
			t.Fatal(c)
		}
	}
	var installed int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM telegram_bot_lifecycle WHERE menu_installed_at>0").Scan(&installed); err != nil || installed != 2 {
		t.Fatal(installed, err)
	}
	if f.reads.Load() != 0 || len(f.tgCalls) != 0 || len(f.waCalls) != 0 {
		t.Fatal("menu setup sent a message or read")
	}
	var registered int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM bridge_bots").Scan(&registered); err != nil || registered != 2 {
		t.Fatal("master menu setup changed the member-bot registry", registered, err)
	}
}
func TestMenuPartialRetryReopensDatabaseAndKeepsOriginalOwnerScope(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	migratedMenuBot(t, f, "master", 999)
	f.menuFailAt = 2
	if _, err := f.store.QueueMenus(t.Context(), "fixture-account", "partial"); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, "control-menu:partial:master", "control-menu"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	reopened, err := Open(t.Context(), f.store.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	f.store.Close()
	f.store = reopened
	restarted, err := control.NewMenus(reopened, f.menuClients, 456, -900)
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := reopened.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup='control-menu:partial:master'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE state='pending'"); err != nil {
		t.Fatal(err)
	}
	if r := f.run(t, id, "control-menu", restarted.Install); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(f.menuCalls) != 4 || f.meCalls.Load() != 1 || f.menuCalls[1] != f.menuCalls[2] || f.menuCalls[3].Scope != `{"type":"chat_member","chat_id":-100,"user_id":123}` {
		t.Fatal(f.menuCalls, f.meCalls.Load())
	}
}
func TestMenuLostResponsesAndLocalOutcomeFailuresRepeatOnlyOriginalSettings(t *testing.T) {
	for _, failure := range []string{"lost response", "completion commit"} {
		t.Run(failure, func(t *testing.T) {
			f := newRelayFixture(t, nil, true)
			migratedMenuBot(t, f, "master", 999)
			if failure == "lost response" {
				f.menuDropAt = 1
			} else {
				if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE telegram_bot_lifecycle ADD CONSTRAINT fixture_refusal CHECK (menu_installed_at=0)"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.store.QueueMenus(t.Context(), "fixture-account", "retry"); err != nil {
				t.Fatal(err)
			}
			if r := f.dispatch(t, "control-menu:retry:master", "control-menu"); r.State != delivery.Pending {
				t.Fatal(r)
			}
			if failure == "completion commit" {
				if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE telegram_bot_lifecycle DROP CONSTRAINT fixture_refusal"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE state='pending'"); err != nil {
				t.Fatal(err)
			}
			if r := f.dispatch(t, "control-menu:retry:master", "control-menu"); r.State != delivery.Done {
				t.Fatal(r)
			}
			if len(f.menuCalls) != 4 {
				t.Fatal(f.menuCalls)
			}
			a, b := 0, 1
			if failure == "completion commit" {
				a, b = 2, 3
			}
			if f.menuCalls[a] != f.menuCalls[b] {
				t.Fatal("settings changed on retry", f.menuCalls)
			}
			var effects int
			if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM effects").Scan(&effects); err != nil || effects != 0 {
				t.Fatal("settings altered message-send uncertainty ledger", effects, err)
			}
		})
	}
}
func TestMenuRejectsUnmigratedAndWrongNativeIdentityAndFencesStaleWrites(t *testing.T) {
	for _, kind := range []string{"unmigrated", "wrong identity", "stale"} {
		t.Run(kind, func(t *testing.T) {
			f := newRelayFixture(t, nil, true)
			if kind != "unmigrated" {
				native := int64(999)
				if kind == "wrong identity" {
					native = 888
				}
				migratedMenuBot(t, f, "master", native)
			}
			payload := json.RawMessage(`{"bot":"master"}`)
			if _, err := f.store.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Dedup: "control-menu:bad:master", Stream: "control-menu:master", Kind: "control-menu", Payload: payload}); err != nil {
				t.Fatal(err)
			}
			if kind == "stale" {
				job := claim(t, f.store)
				other := job
				other.AccountID = "other-account"
				if _, _, err := f.store.MenuPlan(t.Context(), other); !errors.Is(err, delivery.ErrClaimLost) {
					t.Fatal(err)
				}
				expire(t, f.store, job)
				if err := f.store.AdvanceMenu(t.Context(), job, 0); !errors.Is(err, delivery.ErrClaimLost) {
					t.Fatal(err)
				}
			} else {
				if r := f.dispatch(t, "control-menu:bad:master", "control-menu"); r.State != delivery.Failed {
					t.Fatal(r)
				}
			}
			if len(f.menuCalls) != 0 {
				t.Fatal(f.menuCalls)
			}
		})
	}
}
