package postgres

import (
	"errors"
	"strings"
	"testing"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

func TestHistoryRestrictedBotMentionKeepsFrozenTextAndSeparateFallbackReceipt(t *testing.T) {
	for _, outcome := range []string{"accepted", "unknown", "changed username", "rate"} {
		t.Run(outcome, func(t *testing.T) {
			h := newHistoryFixture(t)
			prepareMentions(t, h.f)
			n := newNativeFixture(t, h.f)
			wireNativeHistory(t, h, n)
			h.batch.Store([]sdk.ChatHistoryMessage{{ID: "restricted-mention", ChatID: "peer@g.us", Author: mentionBob, Type: sdk.MsgText, Body: "Hello @12025550123", MentionedIDs: []string{mentionAlice}, Timestamp: 1700000000}})
			n.failMethod, n.failCode, n.failDescription, n.failOnlyNativeMentions = "sendMessage", 400, "Bad Request: BOT_ACCESS_RESTRICTED", true
			if outcome == "unknown" {
				n.dropMentionLinks = true
			}
			if outcome == "changed username" {
				n.mentionUsername = "renamed_bot"
			}
			if outcome == "rate" {
				n.failCode = 429
			}
			ids := fetchNativeHistory(t, h, "all")
			if len(ids) != 1 {
				t.Fatal(ids)
			}
			r := h.f.run(t, ids[0], "history-message", h.service.Item)
			want := delivery.Done
			if outcome == "unknown" {
				want = delivery.Uncertain
			}
			if outcome == "changed username" {
				want = delivery.Failed
			}
			if outcome == "rate" {
				want = delivery.Pending
			}
			if r.State != want {
				t.Fatal(r)
			}
			var originals, variants int
			if err := h.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FILTER(WHERE state='discarded'),count(*) FILTER(WHERE effect_key LIKE '%:original') FROM effects WHERE job_id=$1", ids[0]).Scan(&originals, &variants); err != nil {
				t.Fatal(err)
			}
			if outcome == "changed username" || outcome == "rate" {
				if originals != 0 || variants != 0 {
					t.Fatal("unverified or rate-limited mention changed", originals, variants)
				}
				return
			}
			if originals != 1 || variants != 1 {
				t.Fatal("fallback did not retain distinct original evidence", originals, variants)
			}
			var originalText, fallbackText, originalEntities, fallbackEntities, variantState string
			if err := h.f.store.pool.QueryRow(t.Context(), `SELECT request->'Body'->>'text',(request->'Body'->'entities')::text FROM effects WHERE job_id=$1 AND state='discarded'`, ids[0]).Scan(&originalText, &originalEntities); err != nil {
				t.Fatal(err)
			}
			if err := h.f.store.pool.QueryRow(t.Context(), `SELECT request->'Body'->>'text',(request->'Body'->'entities')::text,state FROM effects WHERE job_id=$1 AND effect_key LIKE '%:original'`, ids[0]).Scan(&fallbackText, &fallbackEntities, &variantState); err != nil {
				t.Fatal(err)
			}
			if originalText != fallbackText || !strings.Contains(originalEntities, "text_mention") || !strings.Contains(fallbackEntities, "https://t.me/alice_bot") || strings.Contains(fallbackEntities, "text_mention") {
				t.Fatal("fallback replaced frozen text or identity")
			}
			if outcome == "unknown" {
				if variantState != "uncertain" {
					t.Fatal(variantState)
				}
				if err := h.f.store.Review(t.Context(), ids[0], true, false); !errors.Is(err, delivery.ErrUncertain) {
					t.Fatal("unknown link send was retryable without evidence", err)
				}
			} else {
				if variantState != "done" || h.reads.Load() != 1 {
					t.Fatal(variantState, h.reads.Load())
				}
				var copies int
				if err := h.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_mappings WHERE whatsapp_message_id='restricted-mention'").Scan(&copies); err != nil || copies != 1 {
					t.Fatal("fallback created incorrect copies", copies, err)
				}
			}
		})
	}
}
