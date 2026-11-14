package postgres

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

func operatorCommand(t *testing.T, f *relayFixture, id int64, bot string, chat, thread int64, text string) delivery.Report {
	t.Helper()
	m := pickerMessage(int(id)+10000, chat, thread, text)
	m.Date = int(time.Now().Unix())
	if !admitPicker(t, f, bot, models.Update{ID: id, Message: m}) {
		t.Fatal("operator command was not admitted")
	}
	return f.dispatch(t, fmt.Sprintf("tg:%s:%d", bot, id), "tg")
}

func operatorKey(bot string, id int64) string { return fmt.Sprintf("management:tg:%s:%d", bot, id) }

func operatorMode(t *testing.T, f *relayFixture, chat string) string {
	t.Helper()
	r, found, err := f.store.ConversationRoute(t.Context(), "fixture-account", chat)
	if err != nil || !found {
		t.Fatal(r, found, err)
	}
	return string(r.Mode)
}

func TestOperatorPreferencesSuppressAndResumeRelaysWithoutReadingWhatsApp(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.route(t, "peer@g.us", -100, 44)
	for i, step := range []struct {
		command, mode, text string
		suppressed          bool
	}{
		{"mute", "muted", "Conversation muted. Use /keep to resume.", true},
		{"keep", "inbox", "Conversation stays here.", false},
		{"ignore", "ignored", "Conversation ignored. Use /keep to resume.", true},
		{"keep", "inbox", "Conversation stays here.", false},
	} {
		id := int64(100 + i)
		before := operatorMode(t, f, "peer@g.us")
		if r := operatorCommand(t, f, id, "master", -100, 44, "/"+step.command); r.State != delivery.Done {
			t.Fatal(r)
		}
		if got := operatorMode(t, f, "peer@g.us"); got != before {
			t.Fatal("source command applied management action inline", got)
		}
		if r := f.dispatch(t, operatorKey("master", id), "management"); r.State != delivery.Done {
			t.Fatal(r)
		}
		if got := operatorMode(t, f, "peer@g.us"); got != step.mode {
			t.Fatal(got, step.mode)
		}
		last := f.tgCalls[len(f.tgCalls)-1]
		if last.Text != step.text || last.Bot != "master-fixture" || last.Chat != -100 || last.Thread != 44 {
			t.Fatal(last)
		}
		n := len(f.tgCalls)
		if r := f.incoming(t, fmt.Sprintf("incoming-%d", i), relay.Incoming{ChatID: "peer@g.us", MessageID: fmt.Sprint(i), Body: "body"}); r.State != delivery.Done {
			t.Fatal(r)
		}
		want := n + 1
		if step.suppressed {
			want = n
		}
		if len(f.tgCalls) != want || f.reads.Load() != 0 || len(f.waCalls) != 0 {
			t.Fatal("preference changed relay/read behavior", f.tgCalls, f.reads.Load(), f.waCalls)
		}
	}
	var commands, timers int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM telegram_observations WHERE kind='command' AND expires_at>0").Scan(&commands); err != nil {
		t.Fatal(err)
	}
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='control-expire'").Scan(&timers); err != nil || commands != 4 || timers != 8 {
		t.Fatal(commands, timers, err)
	}
}

func TestOperatorKeepPreservesBaselineModeSelection(t *testing.T) {
	for _, tc := range []struct{ old, want string }{{"group", "group"}, {"dedicated", "inbox"}, {"muted", "inbox"}, {"ignored", "inbox"}} {
		t.Run(tc.old, func(t *testing.T) {
			f := newRelayFixture(t, nil, true)
			f.route(t, "peer@g.us", -200, 0)
			if _, err := f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET mode=$1", tc.old); err != nil {
				t.Fatal(err)
			}
			// A whole-group route owns the command despite Telegram carrying a
			// reply thread ID. Existing extra arguments are intentionally ignored.
			if r := operatorCommand(t, f, 200, "master", -200, 999, "/keep ignored arguments"); r.State != delivery.Done {
				t.Fatal(r)
			}
			if r := f.dispatch(t, operatorKey("master", 200), "management"); r.State != delivery.Done {
				t.Fatal(r)
			}
			if got := operatorMode(t, f, "peer@g.us"); got != tc.want {
				t.Fatal(got, tc.want)
			}
			last := f.tgCalls[len(f.tgCalls)-1]
			if last.Thread != 0 || last.Chat != -200 {
				t.Fatal(last)
			}
		})
	}
}

func TestOperatorPrivateMemberCommandUsesBoundStableConversationAndMasterConfirmation(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	const lid = "87654321@lid"
	f.route(t, lid, -100, 44)
	if _, err := f.store.pool.Exec(t.Context(), "INSERT INTO contact_aliases(account_id,alias,canonical) VALUES('fixture-account',$1,'12025550100@c.us')", lid); err != nil {
		t.Fatal(err)
	}
	f.bind(t, "200", "12025550100@c.us")
	if r := operatorCommand(t, f, 300, "200", 123, 0, "/ignore"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("200", 300), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if operatorMode(t, f, lid) != "ignored" {
		t.Fatal("alias command created or changed the wrong route")
	}
	if len(f.tgCalls) != 1 || f.tgCalls[0].Bot != "master-fixture" || f.tgCalls[0].Chat != -100 || f.tgCalls[0].Thread != 44 {
		t.Fatal(f.tgCalls)
	}
	var command int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM telegram_observations WHERE scope='200' AND chat_id=123 AND kind='command'").Scan(&command); err != nil || command != 1 {
		t.Fatal(command, err)
	}
}

func TestOperatorReplyRetryCannotUndoLaterDecisionOrMoveItsConfirmation(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.route(t, "peer@g.us", -100, 44)
	f.failTG = 1
	if r := operatorCommand(t, f, 400, "master", -100, 44, "/mute"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 400), "management"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	if operatorMode(t, f, "peer@g.us") != "muted" {
		t.Fatal("mode was rolled back with notice refusal")
	}
	if r := operatorCommand(t, f, 401, "master", -100, 44, "/keep"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 401), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	f.route(t, "peer@g.us", -200, 0)
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE dedup=$1", operatorKey("master", 400)); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, operatorKey("master", 400), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if operatorMode(t, f, "peer@g.us") != "inbox" {
		t.Fatal("old notice re-applied mute")
	}
	first, last := f.tgCalls[0], f.tgCalls[len(f.tgCalls)-1]
	if first != last || last.Chat != -100 || last.Thread != 44 {
		t.Fatal("notice did not keep original target and text", first, last)
	}
}

func TestOperatorNoticeRecoveryReopensDatabaseWithoutRepeatingTheModeChange(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.route(t, "peer@g.us", -100, 44)
	f.failTG = 1
	if r := operatorCommand(t, f, 450, "master", -100, 44, "/mute"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 450), "management"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	reopened, err := Open(t.Context(), f.store.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	f.store.Close()
	f.store = reopened
	replies, err := control.New(reopened, f.replyClients, -100)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := control.NewOperator(reopened, replies, control.OperatorOptions{OwnerID: 123})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.pool.Exec(t.Context(), "UPDATE bridge_routes SET mode='inbox'; UPDATE jobs SET due=clock_timestamp() WHERE state='pending'"); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := reopened.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", operatorKey("master", 450)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if r := f.run(t, id, "management", restarted.Manage); r.State != delivery.Done {
		t.Fatal(r)
	}
	if operatorMode(t, f, "peer@g.us") != "inbox" || len(f.tgCalls) != 2 || f.tgCalls[0] != f.tgCalls[1] {
		t.Fatal(f.tgCalls)
	}
}

func TestOperatorLostNoticeResponseRequiresReviewAndDoesNotBlockLaterPreference(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.route(t, "peer@g.us", -100, 44)
	f.dropTGAt = 1
	if r := operatorCommand(t, f, 460, "master", -100, 44, "/mute"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 460), "management"); r.State != delivery.Uncertain {
		t.Fatal(r)
	}
	if r := operatorCommand(t, f, 461, "master", -100, 44, "/keep"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 461), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	var state string
	if err := f.store.pool.QueryRow(t.Context(), "SELECT state FROM jobs WHERE dedup=$1", operatorKey("master", 460)).Scan(&state); err != nil || state != "uncertain" {
		t.Fatal(state, err)
	}
	if operatorMode(t, f, "peer@g.us") != "inbox" || len(f.tgCalls) != 2 {
		t.Fatal(f.tgCalls)
	}
}

func TestOperatorAdmissionAndPreferencePlansRollbackTogether(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.route(t, "peer@g.us", -100, 44)
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans ADD CONSTRAINT fixture_refusal CHECK (payload->'Operator' = 'null'::jsonb)"); err != nil {
		t.Fatal(err)
	}
	if r := operatorCommand(t, f, 500, "master", -100, 44, "/mute"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	var jobs, commands int
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE dedup=$1", operatorKey("master", 500)).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM telegram_observations WHERE kind='command' AND expires_at>0").Scan(&commands); err != nil || jobs != 0 || commands != 0 {
		t.Fatal(jobs, commands, err)
	}
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans DROP CONSTRAINT fixture_refusal; UPDATE jobs SET due=clock_timestamp() WHERE state='pending'"); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, "tg:master:500", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	// Failure to save the management confirmation must also roll back its mode
	// write, so no owner state can change without a frozen recovery plan.
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans ADD CONSTRAINT fixture_refusal CHECK (plan_key NOT LIKE 'management:%')"); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, operatorKey("master", 500), "management"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	if operatorMode(t, f, "peer@g.us") != "inbox" || len(f.tgCalls) != 0 {
		t.Fatal("mode escaped the failed plan transaction")
	}
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE control_plans DROP CONSTRAINT fixture_refusal; UPDATE jobs SET due=clock_timestamp() WHERE state='pending'"); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, operatorKey("master", 500), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(f.tgCalls) != 1 || operatorMode(t, f, "peer@g.us") != "muted" {
		t.Fatal(f.tgCalls)
	}
}

func TestOperatorReadIsExplicitDurableAndIndependentOfTextSending(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.route(t, "peer@g.us", -100, 44)
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET mode='muted'"); err != nil {
		t.Fatal(err)
	}
	f.readFailure.Store(true)
	if r := operatorCommand(t, f, 600, "master", -100, 44, "/read"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if f.reads.Load() != 0 {
		t.Fatal("source command performed a provider call")
	}
	if r := f.dispatch(t, operatorKey("master", 600), "management"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	f.readFailure.Store(false)
	if _, err := f.store.pool.Exec(t.Context(), "UPDATE jobs SET due=clock_timestamp() WHERE dedup=$1", operatorKey("master", 600)); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, operatorKey("master", 600), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if f.reads.Load() != 2 || len(f.readChats) != 2 || f.readChats[0] != "peer@g.us" || f.readChats[1] != "peer@g.us" || len(f.tgCalls) != 0 || len(f.waCalls) != 0 || operatorMode(t, f, "peer@g.us") != "muted" {
		t.Fatal(f.readChats, f.tgCalls, f.waCalls)
	}
	if admitPicker(t, f, "master", models.Update{ID: 600, Message: pickerMessage(10600, -100, 44, "/read")}) {
		t.Fatal("duplicate source command was admitted")
	}
}

func TestOperatorCommandsRequireLinkedContextAndCorrectAddress(t *testing.T) {
	for _, tc := range []struct {
		name, bot, text       string
		chat, thread          int64
		route                 bool
		wantJobs, wantReplies int
	}{
		{"unknown topic", "master", "/mute", -100, 45, true, 0, 1},
		{"master DM", "master", "/ignore", 123, 0, false, 0, 1},
		{"unbound member DM", "200", "/keep", 123, 0, false, 0, 1},
		{"unqualified member group", "200", "/mute", -100, 44, true, 0, 0},
		{"another recipient", "master", "/mute@somewhere_else_bot", -100, 44, true, 0, 0},
		{"addressed member group", "200", "/ignore@alice_bot", -100, 44, true, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRelayFixture(t, nil, true)
			if tc.route {
				f.route(t, "peer@g.us", -100, 44)
			}
			if tc.bot == "200" {
				if err := f.store.PutIngressBot(t.Context(), "fixture-account", ingress.Bot{ID: "200", Username: "alice_bot"}); err != nil {
					t.Fatal(err)
				}
			}
			u := models.Update{ID: 700, Message: pickerMessage(10700, tc.chat, tc.thread, tc.text)}
			admitted := admitPicker(t, f, tc.bot, u)
			if admitted {
				if r := f.dispatch(t, "tg:"+tc.bot+":700", "tg"); r.State != delivery.Done {
					t.Fatal(r)
				}
			}
			var jobs int
			if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='management'").Scan(&jobs); err != nil || jobs != tc.wantJobs || len(f.tgCalls) != tc.wantReplies {
				t.Fatal(jobs, f.tgCalls, err)
			}
			if tc.wantReplies > 0 && f.tgCalls[0].Text != "Use this command inside a linked conversation or its management topic." {
				t.Fatal(f.tgCalls)
			}
		})
	}
}

func TestOperatorRejectsForeignRepliesAndUnauthorizedActors(t *testing.T) {
	for _, kind := range []string{"foreign reply", "wrong owner", "bot actor"} {
		t.Run(kind, func(t *testing.T) {
			f := newRelayFixture(t, nil, true)
			f.route(t, "peer@g.us", -100, 44)
			m := pickerMessage(10800, -100, 44, "/mute")
			switch kind {
			case "foreign reply":
				if _, err := f.store.pool.Exec(t.Context(), "INSERT INTO message_mappings(account_id,scope,telegram_chat_id,telegram_message_id,thread_id,bot_id,whatsapp_chat,whatsapp_message_id,direction,receipt) VALUES('fixture-account','*',-100,7,44,'master','other@g.us','other','in','{}')"); err != nil {
					t.Fatal(err)
				}
				m.ReplyToMessage = pickerMessage(7, -100, 44, "")
			case "wrong owner":
				m.From.ID = 999
			case "bot actor":
				m.From.IsBot = true
			}
			if admitPicker(t, f, "master", models.Update{ID: 800, Message: m}) {
				r := f.dispatch(t, "tg:master:800", "tg")
				if kind == "foreign reply" && r.State != delivery.Failed {
					t.Fatal(r)
				}
			}
			var n int
			if err := f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='management'").Scan(&n); err != nil || n != 0 || operatorMode(t, f, "peer@g.us") != "inbox" || len(f.tgCalls) != 0 {
				t.Fatal(n, f.tgCalls, err)
			}
		})
	}
}

func TestOperatorCaptionCommandsAndFormerCommandEditsNeverRelay(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.route(t, "peer@g.us", -100, 44)
	m := pickerMessage(10900, -100, 44, "")
	m.Caption = "/ignore"
	m.Document = &models.Document{FileID: "fixture-file"}
	if !admitPicker(t, f, "master", models.Update{ID: 900, Message: m}) {
		t.Fatal("caption command not admitted")
	}
	if r := f.dispatch(t, "tg:master:900", "tg"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if r := f.dispatch(t, operatorKey("master", 900), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	for i, text := range []string{"/keep", "edited into ordinary text"} {
		u := models.Update{ID: int64(901 + i), EditedMessage: pickerMessage(10900, -100, 44, text)}
		if !admitPicker(t, f, "master", u) {
			t.Fatal("edited command not admitted")
		}
		if r := f.dispatch(t, fmt.Sprintf("tg:master:%d", u.ID), "tg"); r.State != delivery.Done {
			t.Fatal(r)
		}
	}
	if operatorMode(t, f, "peer@g.us") != "ignored" || len(f.waCalls) != 0 || len(f.tgCalls) != 1 {
		t.Fatal(f.waCalls, f.tgCalls)
	}
}

func TestOperatorPreparedManagementIsFencedAndDoesNotRepeatAfterReceipt(t *testing.T) {
	f := newRelayFixture(t, nil, true)
	f.route(t, "peer@g.us", -100, 44)
	if r := operatorCommand(t, f, 1000, "master", -100, 44, "/mute"); r.State != delivery.Done {
		t.Fatal(r)
	}
	job := claim(t, f.store)
	if job.Kind != "management" {
		t.Fatal(job.Kind)
	}
	other := job
	other.AccountID = "other-account"
	if _, err := f.store.PrepareOperatorControl(t.Context(), other, 60); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
	expire(t, f.store, job)
	if _, err := f.store.PrepareOperatorControl(t.Context(), job, 60); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
	if _, err := f.store.RecoverExpired(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Fail the local observation write after a confirmed send receipt.
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE telegram_observations ADD CONSTRAINT fixture_refusal CHECK (kind<>'log')"); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, operatorKey("master", 1000), "management"); r.State != delivery.Pending {
		t.Fatal(r)
	}
	if _, err := f.store.pool.Exec(t.Context(), "ALTER TABLE telegram_observations DROP CONSTRAINT fixture_refusal; UPDATE bridge_routes SET mode='inbox'; UPDATE jobs SET due=clock_timestamp() WHERE state='pending'"); err != nil {
		t.Fatal(err)
	}
	if r := f.dispatch(t, operatorKey("master", 1000), "management"); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(f.tgCalls) != 1 || operatorMode(t, f, "peer@g.us") != "inbox" {
		t.Fatal("receipt recovery resent confirmation or re-applied preference", f.tgCalls)
	}
}
