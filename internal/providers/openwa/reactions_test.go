package openwa

import (
	"encoding/json"
	"errors"
	"fmt"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/providers"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeReactionRemovalKeepsExplicitEmptyEmoji(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if !strings.HasSuffix(r.URL.Path, "/react") || string(payload["emoji"]) != `""` || string(payload["messageId"]) != `"source-id"` {
			t.Error("native removal payload changed", payload)
		}
		fmt.Fprint(w, `{"success":true}`)
	}))
	defer server.Close()
	c, err := New(server.URL, "fixture-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.React(t.Context(), "fixture-session", sdk.ReactMessageRequest{ChatID: "peer@g.us", MessageID: "source-id", Emoji: ""}); err != nil {
		t.Fatal(err)
	}
}
func TestNativeWhatsAppReactionTransportInterruptionIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	}))
	defer server.Close()
	c, err := New(server.URL, "fixture-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = c.React(t.Context(), "fixture-session", sdk.ReactMessageRequest{ChatID: "peer@g.us", MessageID: "source-id", Emoji: "👍"})
	var outcome *providers.Error
	if !errors.As(err, &outcome) || !outcome.Retry || outcome.Uncertain {
		t.Fatal(err)
	}
}
