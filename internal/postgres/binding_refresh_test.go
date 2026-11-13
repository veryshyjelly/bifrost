package postgres

import (
	"encoding/json"
	"testing"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

func TestBindingChoicesReissueKeepsOldExpiryAndRequiresCurrentSetup(t *testing.T) {
	for _, outcome := range []string{"ready", "changed credential", "already bound"} {
		t.Run(outcome, func(t *testing.T) {
			b := newBotFixture(t)
			p := provisioner(t, b)
			b.f.route(t, "12025550123@c.us", -100, 44)
			if r := provisionUpdate(t, b, p, models.Update{ID: 2100, Message: pickerMessage(2101, -100, 44, "/create")}); r.State != delivery.Done {
				t.Fatal(r)
			}
			setup := b.register(t, 2101)
			if r := b.f.run(t, setup, "control-bot-setup", b.service.Setup); r.State != delivery.Done {
				t.Fatal(r)
			}
			if _, err := b.f.store.pool.Exec(t.Context(), "UPDATE jobs SET state='done' WHERE kind='control-menu'"); err != nil {
				t.Fatal(err)
			}
			var original int64
			if err := b.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE kind='control-bot-created'").Scan(&original); err != nil {
				t.Fatal(err)
			}
			if r := b.f.run(t, original, "control-bot-created", p.Created); r.State != delivery.Done {
				t.Fatal(r)
			}
			if _, err := b.f.store.pool.Exec(t.Context(), "UPDATE contact_bind_choices SET expires_at=1"); err != nil {
				t.Fatal(err)
			}
			if outcome == "changed credential" {
				if _, err := b.f.store.pool.Exec(t.Context(), "UPDATE telegram_credentials SET revision='different' WHERE bot_id='200'"); err != nil {
					t.Fatal(err)
				}
			}
			if outcome == "already bound" {
				b.f.bind(t, "200", "12025550123@c.us")
			}
			payload := json.RawMessage(`{"type":"operator_action","data":{"operation":"binding_choices","bot":"200"}}`)
			key := "management:reissue"
			if _, err := b.f.store.Enqueue(t.Context(), delivery.Event{AccountID: "fixture-account", Kind: "management", Dedup: key, Stream: key, Payload: payload}); err != nil {
				t.Fatal(err)
			}
			var id int64
			if err := b.f.store.pool.QueryRow(t.Context(), "SELECT id FROM jobs WHERE dedup=$1", key).Scan(&id); err != nil {
				t.Fatal(err)
			}
			r := b.f.run(t, id, "management", p.Manage)
			want := delivery.Done
			if outcome != "ready" {
				want = delivery.Failed
			}
			if r.State != want {
				t.Fatal(r)
			}
			var old, newChoices int
			if err := b.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FILTER(WHERE expires_at=1),count(*) FILTER(WHERE expires_at>1) FROM contact_bind_choices").Scan(&old, &newChoices); err != nil {
				t.Fatal(err)
			}
			if old != 1 {
				t.Fatal("old choice expiry changed", old)
			}
			if outcome != "ready" {
				if newChoices != 0 {
					t.Fatal("unready/bound bot offered choices")
				}
				return
			}
			if newChoices != 1 {
				t.Fatal(newChoices)
			}
			before := len(b.f.tgCalls)
			if _, err := b.f.store.pool.Exec(t.Context(), "UPDATE jobs SET state='pending' WHERE id=$1", id); err != nil {
				t.Fatal(err)
			}
			if r := b.f.run(t, id, "management", p.Manage); r.State != delivery.Done {
				t.Fatal(r)
			}
			var after int
			if err := b.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM contact_bind_choices").Scan(&after); err != nil || after != 2 || len(b.f.tgCalls) != before {
				t.Fatal("reissue replay duplicated choice or notice", after, err)
			}
			var bound int
			if err := b.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM bridge_contact_bots").Scan(&bound); err != nil || bound != 0 {
				t.Fatal("reissue selected a contact", bound, err)
			}
		})
	}
}
