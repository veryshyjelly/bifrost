package statuses

import (
	"encoding/json"
	"testing"
	"time"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
)

func TestDecodeUsesNativeTimesAndRetainsUnavailableCaption(t *testing.T) {
	now := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC)
	raw := json.RawMessage(`{"statusId":"story","contact":{"id":"12345000000001@lid","name":"Alice <a>"},"type":"image","caption":"Keep this caption","postedAt":1794443400000,"expiresAt":"2026-11-13T00:00:00Z","hasMedia":false,"mediaUrl":"https://untrusted.invalid/tracker"}`)
	p, ok := Decode(raw, now)
	if !ok || p.Body != "Keep this caption" || !p.MediaOmitted || p.Name != "Alice <a>" || p.PostedAt != 1794443400000 {
		t.Fatal(p, ok)
	}
	m := p.Message()
	if m.ChatID != ChatID || m.Author != p.Author || m.Type != sdk.MsgImage || !m.Media.Omitted || m.Media.Data != "" || m.Timestamp != p.PostedAt/1000 {
		t.Fatal(m)
	}
	s := p.Source("")
	if s.LinkSenderID != p.Author || s.Own || s.ChatID != ChatID {
		t.Fatal(s)
	}
	frozen, _ := json.Marshal(p)
	var decoded map[string]any
	if json.Unmarshal(frozen, &decoded) != nil {
		t.Fatal("invalid frozen post")
	}
	if _, ok := decoded["mediaUrl"]; ok {
		t.Fatal("external URL survived normalization")
	}
}

func TestDecodeRejectsInvalidOrExpiredPostsAndKeepsUnsupportedLabel(t *testing.T) {
	now := time.UnixMilli(1700000000000)
	base := map[string]any{"statusId": "story", "contact": map[string]string{"id": "123@c.us"}, "type": "document", "caption": "Original", "postedAt": now.UnixMilli(), "expiresAt": now.Add(time.Hour).UnixMilli()}
	raw, _ := json.Marshal(base)
	p, ok := Decode(raw, now)
	if !ok || p.Kind != "text" || p.Body != "Original\n[document status]" {
		t.Fatal(p, ok)
	}
	for _, mutation := range []map[string]any{{"statusId": ""}, {"contact": map[string]string{"id": "123@g.us"}}, {"expiresAt": now.UnixMilli()}, {"postedAt": 1.5}, {"postedAt": 9007199254740991.0}, {"postedAt": "bad"}} {
		input := map[string]any{}
		for k, v := range base {
			input[k] = v
		}
		for k, v := range mutation {
			input[k] = v
		}
		raw, _ := json.Marshal(input)
		if p, ok := Decode(raw, now); ok {
			t.Fatal("invalid status accepted", p)
		}
	}
}

func TestFromRecordDoesNotResolveExternalMediaURL(t *testing.T) {
	now := time.Now()
	r := sdk.StatusRecord{ID: "story", Contact: sdk.StatusContact{ID: "123@s.whatsapp.net", PushName: "Alice"}, Type: "voice", Caption: "Voice", Timestamp: now, MediaURL: "https://untrusted.invalid/file"}
	p, ok := FromRecord(r, now)
	if !ok || !p.HasMedia || p.MediaOmitted || p.ExpiresAt != r.Timestamp.UnixMilli()+86400000 || p.Name != "Alice" || p.Message().Type != sdk.MsgVoice {
		t.Fatal(p, ok)
	}
}
