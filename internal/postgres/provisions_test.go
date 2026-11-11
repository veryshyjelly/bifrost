package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/bots"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

func provisioner(t *testing.T, b *botFixture) *bots.Provisioner {
	t.Helper()
	replies, err := control.New(b.f.store, b.f.replyClients, -100)
	if err != nil {
		t.Fatal(err)
	}
	master, err := b.f.replyClients(t.Context(), "fixture-account", "master")
	if err != nil {
		t.Fatal(err)
	}
	p, err := bots.NewProvisioner(b.f.store, replies, master, b.settings)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func provisionUpdate(t *testing.T, b *botFixture, p *bots.Provisioner, u models.Update) delivery.Report {
	t.Helper()
	if !admitPicker(t, b.f, "master", u) {
		t.Fatal("provision update not admitted")
	}
	var id int64
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", fmt.Sprintf("tg:master:%d", u.ID)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return b.f.run(t, id, "tg", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		return p.Handle(ctx, j, e, "master", &u)
	})
}
func TestProvisionCreatePreservesMasterRouteAndFreezesPrompt(t *testing.T) {
	b := newBotFixture(t)
	p := provisioner(t, b)
	b.f.route(t, "12025550123@c.us", -100, 44)
	u := models.Update{ID: 100, Message: pickerMessage(101, -100, 44, "/create")}
	if r := provisionUpdate(t, b, p, u); r.State != delivery.Done {
		t.Fatal(r)
	}
	if operatorMode(t, b.f, "12025550123@c.us") != "inbox" {
		t.Fatal("creation changed routing before binding")
	}
	b.f.mu.Lock()
	calls := append([]tgTextCall(nil), b.f.tgCalls...)
	b.f.mu.Unlock()
	if len(calls) != 1 || !strings.Contains(calls[0].Text, "bot limit") || !strings.Contains(calls[0].Markup, "https://t.me/newbot/") {
		t.Fatal(calls)
	}
	var count int
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM contact_provisions WHERE state='pending'").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
func TestProvisionNativeGroupCreateUsesMappedParticipant(t *testing.T) {
	b := newBotFixture(t)
	p := provisioner(t, b)
	b.f.route(t, "peer@g.us", -100, 44)
	// Admit a native source so its immutable sender identity backs the mapping.
	body := `{"event":"message.received","sessionId":"fixture-session","idempotencyKey":"provision-source","data":{"id":"wa-participant","chatId":"peer@g.us","author":"12345000000001@lid","body":"Hello","type":"text","isGroup":true,"timestamp":1700000000}}`
	if !fixtureIngress(t, b.f)("openwa", []byte(body)) {
		t.Fatal("source not admitted")
	}
	if r := b.f.dispatch(t, "ow:provision-source", "wa"); r.State != delivery.Done {
		t.Fatal(r)
	}
	u := models.Update{ID: 101, Message: pickerMessage(102, -100, 44, "/create")}
	u.Message.ReplyToMessage = &models.Message{ID: 1001}
	if r := provisionUpdate(t, b, p, u); r.State != delivery.Done {
		t.Fatal(r)
	}
	var person string
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT person_id FROM contact_provisions").Scan(&person); err != nil || person != "12345000000001@lid" {
		t.Fatal(person, err)
	}
}
func TestProvisionBindingIsScopedSingleUseAndWaitsForStartBeforeRetirement(t *testing.T) {
	b := newBotFixture(t)
	p := provisioner(t, b)
	b.f.route(t, "12025550123@c.us", -100, 44)
	if r := provisionUpdate(t, b, p, models.Update{ID: 102, Message: pickerMessage(103, -100, 44, "/create")}); r.State != delivery.Done {
		t.Fatal(r)
	}
	setup := b.register(t, 103)
	if r := b.f.run(t, setup, "control-bot-setup", b.service.Setup); r.State != delivery.Done {
		t.Fatal(r)
	}
	// Settings/menu tests are independent; release the menu predecessor here.
	if _, err := b.f.store.pool.Exec(t.Context(), "UPDATE jobs SET state='done' WHERE kind='control-menu'"); err != nil {
		t.Fatal(err)
	}
	var notice int64
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='control-bot-created'").Scan(&notice); err != nil {
		t.Fatal(err)
	}
	if r := b.f.run(t, notice, "control-bot-created", p.Created); r.State != delivery.Done {
		t.Fatal(r)
	}
	var action, planKey string
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT choice_id,plan_key FROM contact_bind_choices").Scan(&action, &planKey); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT result FROM effects WHERE effect_key=$1", planKey+":text:0").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var receipt models.Message
	if json.Unmarshal(raw, &receipt) != nil {
		t.Fatal("bad fixture receipt")
	}
	callback := func(id int64, chat int64) models.Update {
		m := pickerMessage(receipt.ID, chat, 0, "")
		return models.Update{ID: id, CallbackQuery: &models.CallbackQuery{ID: fmt.Sprint(id), From: models.User{ID: 123}, Data: "bind:" + action, Message: models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeMessage, Message: m}}}
	}
	if r := provisionUpdate(t, b, p, callback(104, -200)); r.State != delivery.Done {
		t.Fatal(r)
	}
	var bound int
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM bridge_contact_bots").Scan(&bound); err != nil || bound != 0 {
		t.Fatal("foreign callback bound contact", bound, err)
	}
	if r := provisionUpdate(t, b, p, callback(105, 123)); r.State != delivery.Done {
		t.Fatal(r)
	}
	if operatorMode(t, b.f, "12025550123@c.us") != "dedicated" {
		t.Fatal("binding preference missing")
	}
	var retire int
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE kind='control-retire-inbox'").Scan(&retire); err != nil || retire != 0 {
		t.Fatal("unstarted bot retired inbox", retire, err)
	}
	if r := provisionUpdate(t, b, p, callback(106, 123)); r.State != delivery.Done {
		t.Fatal(r)
	}
	var syncs int
	if err := b.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE dedup LIKE 'binding-sync:%'").Scan(&syncs); err != nil || syncs != 1 {
		t.Fatal("binding repeated", syncs, err)
	}
	if r := b.f.incoming(t, "after-binding", relay.Incoming{ChatID: "12025550123@c.us", MessageID: "still-master", Body: "Hello"}); r.State != delivery.Done {
		t.Fatal(r)
	}
	b.f.mu.Lock()
	last := b.f.tgCalls[len(b.f.tgCalls)-1]
	b.f.mu.Unlock()
	if last.Chat != -100 || last.Thread != 44 {
		t.Fatal("unstarted bot stole master routing", last)
	}
}
