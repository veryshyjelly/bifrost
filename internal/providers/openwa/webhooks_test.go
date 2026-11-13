package openwa

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebhookSetupOwnsExactURLAndRestoresUnfilteredSignedDelivery(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "update"}[existing], func(t *testing.T) {
			writes := 0
			callback := "http://bifrost.invalid/webhooks/openwa"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "GET" {
					if existing {
						_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "owned", "sessionId": "session", "url": callback}, {"id": "unrelated", "sessionId": "session", "url": "http://other.invalid"}})
					} else {
						_, _ = w.Write([]byte(`[]`))
					}
					return
				}
				writes++
				var body map[string]json.RawMessage
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("invalid settings JSON")
				}
				var secret string
				_ = json.Unmarshal(body["secret"], &secret)
				if secret != strings.Repeat("s", 32) {
					t.Error("signing secret lost")
				}
				if existing {
					if r.Method != "PUT" || !strings.HasSuffix(r.URL.Path, "/owned") || string(body["filters"]) != "null" || string(body["headers"]) != "{}" || string(body["active"]) != "true" {
						t.Error("existing callback constraints were retained")
					}
				} else if r.Method != "POST" {
					t.Error("creation used wrong method")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "owned", "sessionId": "session", "url": callback, "active": true, "events": []string{"message.received", "status.received"}})
			}))
			defer server.Close()
			client, err := New(server.URL, "fixture", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = client.EnsureWebhook(t.Context(), "session", callback, strings.Repeat("s", 32), []string{"status.received", "message.received"}); err != nil || writes != 1 {
				t.Fatal("native callback setup failed", err)
			}
		})
	}
}

func TestWebhookSetupRefusesDuplicateOwnershipBeforeWriting(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"one","sessionId":"session","url":"http://bifrost.invalid/hook"},{"id":"two","sessionId":"session","url":"http://bifrost.invalid/hook"}]`))
	}))
	defer server.Close()
	client, err := New(server.URL, "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.EnsureWebhook(t.Context(), "session", "http://bifrost.invalid/hook", strings.Repeat("s", 32), []string{"message.received"}); err == nil || writes != 0 {
		t.Fatal("ambiguous callbacks changed", err)
	}
}
