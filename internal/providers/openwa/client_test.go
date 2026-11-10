package openwa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/rmyndharis/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func TestSDKSendPreservesKeyQuoteAndPayloadAndEmptyCaption(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-API-Key") != "private-key" {
			t.Error("missing API key")
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if strings.HasSuffix(r.URL.Path, "/send-text") {
			if r.Header.Get("Idempotency-Key") != "stable-key" || string(payload["quotedMessageId"]) != `"quoted-id"` || string(payload["text"]) != `"hello"` {
				t.Error("send lost key, text or quote")
			}
		} else if string(payload["body"]) != `""` {
			t.Error("empty caption omitted")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"messageId":"wa-id","timestamp":123}`)
	}))
	defer server.Close()
	client, err := New(server.URL, "private-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.SendText(t.Context(), "session", sdk.SendTextRequest{ChatID: "fixture@g.us", Text: "hello", QuotedMessageID: "quoted-id"}, "stable-key")
	if err != nil || result.MessageID != "wa-id" {
		t.Fatalf("send result error=%v", err)
	}
	if _, err := client.Edit(t.Context(), "session", sdk.EditMessageRequest{ChatID: "fixture@g.us", MessageID: "wa-id", Body: ""}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("unexpected retries")
	}
}

func TestSDKFailuresAreClassifiedWithoutLeakingProviderBodies(t *testing.T) {
	for _, tc := range []struct {
		name             string
		status           int
		body             string
		retry, uncertain bool
		delay            time.Duration
	}{
		{"rate", 429, `{"message":"private message","retryAfterSeconds":3}`, true, false, 3 * time.Second},
		{"pending", 409, `{"code":"IDEMPOTENCY_KEY_IN_PROGRESS"}`, true, false, 5 * time.Second},
		{"unknown", 409, `{"code":"IDEMPOTENCY_OUTCOME_UNKNOWN"}`, false, true, 0},
		{"engine conflict", 409, `{"message":"private message"}`, false, true, 0},
		{"permission", 403, `{"code":"private message","message":"private message"}`, false, false, 0},
		{"unsupported", 501, `{"message":"private message"}`, false, false, 0},
		{"server", 503, `{"message":"private message"}`, false, true, 0},
		{"malformed success", 200, `private message`, false, true, 0},
		{"missing receipt", 200, `{}`, false, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client, _ := New(server.URL, "private-key", nil)
			_, err := client.SendText(t.Context(), "session", sdk.SendTextRequest{ChatID: "fixture@g.us", Text: "hello"}, "stable-key")
			var outcome *providers.Error
			if !errors.As(err, &outcome) {
				t.Fatalf("missing safe error: %v", err)
			}
			if outcome.Retry != tc.retry || outcome.Uncertain != tc.uncertain || outcome.RetryAfter != tc.delay {
				t.Fatalf("outcome=%+v", outcome)
			}
			if strings.Contains(fmt.Sprintf("%+v", err), "private message") || strings.Contains(err.Error(), "private-key") {
				t.Fatal("private provider data leaked")
			}
			if calls.Load() != 1 {
				t.Fatal("SDK automatically retried mutation")
			}
		})
	}
}

func TestAmbiguousDisconnectDoesNotResendAndRedirectDoesNotLeakKey(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	}))
	defer server.Close()
	client, _ := New(server.URL, "private-key", nil)
	_, err := client.SendText(context.Background(), "session", sdk.SendTextRequest{ChatID: "fixture@g.us", Text: "hello"}, "key")
	var outcome *providers.Error
	if !errors.As(err, &outcome) || !outcome.Uncertain || outcome.Retry || calls.Load() != 1 {
		t.Fatal("ambiguous send replayed")
	}
	var escaped atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { escaped.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer redirect.Close()
	client, _ = New(redirect.URL, "private-key", nil)
	_, _ = client.SendText(t.Context(), "session", sdk.SendTextRequest{ChatID: "fixture@g.us", Text: "hello"}, "key")
	if escaped.Load() != 0 {
		t.Fatal("redirect forwarded credentials")
	}
}

func TestInvalidSendKeyNeverReachesProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, _ := New(server.URL, "private-key", nil)
	for _, key := range []string{"", "bad\nkey", strings.Repeat("a", 256)} {
		if _, err := client.SendText(t.Context(), "session", sdk.SendTextRequest{ChatID: "fixture@g.us", Text: "hello"}, key); !errors.Is(err, providers.ErrInvalidRequest) {
			t.Fatal("invalid idempotency key accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid operation called provider")
	}
}
