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

func TestNativeSourceDeleteUsesExplicitEveryoneAndExactTarget(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if !strings.HasSuffix(r.URL.Path, "/delete") || string(payload["messageId"]) != `"source-message"` || string(payload["forEveryone"]) != "true" {
			t.Error("native source delete changed", payload)
		}
		fmt.Fprint(w, `{"success":true}`)
	}))
	defer server.Close()
	c, err := New(server.URL, "fixture-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	everyone := true
	if err := c.DeleteMessage(t.Context(), "fixture-session", sdk.DeleteMessageRequest{ChatID: "peer@g.us", MessageID: "source-message", ForEveryone: &everyone}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}
func TestNativeSourceDeleteLostResponseRetriesAsIdempotentOperation(t *testing.T) {
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
	everyone := true
	err = c.DeleteMessage(t.Context(), "fixture-session", sdk.DeleteMessageRequest{ChatID: "peer@g.us", MessageID: "source-message", ForEveryone: &everyone})
	var outcome *providers.Error
	if !errors.As(err, &outcome) || !outcome.Retry || outcome.Uncertain {
		t.Fatal(err)
	}
}
