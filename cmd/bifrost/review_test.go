package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/recovery"
)

func TestRecoveryCLIUsesExplicitReviewedFileAndAccountScope(t *testing.T) {
	secret := strings.Repeat("x", 32)
	t.Setenv("BRIDGE_ADMIN_TOKEN", secret)
	t.Setenv("BIFROST_ACCOUNT_ID", "account")
	var reads, writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("missing bearer auth")
		}
		if r.Method == "GET" {
			reads.Add(1)
			if r.URL.Path != "/v1/topic-review" || r.URL.Query().Get("job") != "7" {
				t.Error("wrong inspection target")
			}
			json.NewEncoder(w).Encode(recovery.TopicSnapshot{AccountID: "account", Job: 7, State: "uncertain", Effect: "original:topic", EffectState: "uncertain", RequestDigest: strings.Repeat("a", 64), Management: -100, Name: "Original", Scope: "status"})
			return
		}
		writes.Add(1)
		var d recovery.TopicDecision
		if json.NewDecoder(r.Body).Decode(&d) != nil || !d.Valid() || d.AccountID != "account" || d.ThreadID != 99 {
			t.Error("review input changed")
		}
		json.NewEncoder(w).Encode(recovery.TopicResult{Job: d.Job, Effect: d.Effect, Action: d.Action, ThreadID: d.ThreadID, State: "pending"})
	}))
	defer server.Close()
	var out bytes.Buffer
	if err := recoveryCLI(context.Background(), "inspect-topic", []string{"--job", "7", "--url", server.URL}, &out); err != nil || !strings.Contains(out.String(), `"requestDigest"`) {
		t.Fatal(out.String(), err)
	}
	d := recovery.TopicDecision{OperationID: "decision-1", AccountID: "account", Job: 7, Effect: "original:topic", ExpectedState: "uncertain", ExpectedEffectState: "uncertain", RequestDigest: strings.Repeat("a", 64), Management: -100, Action: "adopt", ThreadID: 99, ConfirmCreated: true}
	file := filepath.Join(t.TempDir(), "review.json")
	raw, _ := json.Marshal(d)
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := recoveryCLI(context.Background(), "reconcile-topic", []string{"--file", file, "--url", server.URL}, &out); err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 1 || writes.Load() != 1 || strings.Contains(out.String(), secret) {
		t.Fatal(reads.Load(), writes.Load(), out.String())
	}
	d.AccountID = "other"
	raw, _ = json.Marshal(d)
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recoveryCLI(context.Background(), "reconcile-topic", []string{"--file", file, "--url", server.URL}, &out); err == nil || writes.Load() != 1 {
		t.Fatal("foreign file reached server", err, writes.Load())
	}
}

func TestRecoveryCLIReviewRequiresExplicitActionAndNeverFollowsRedirects(t *testing.T) {
	secret := strings.Repeat("x", 32)
	t.Setenv("BRIDGE_ADMIN_TOKEN", secret)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Job     int64
			Action  string
			Confirm bool `json:"confirmNoDelivery"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Job != 7 || body.Action != "retry" || !body.Confirm {
			t.Error("retry confirmation changed")
		}
		fmt.Fprint(w, `{"accepted":true}`)
	}))
	defer server.Close()
	var out bytes.Buffer
	if err := recoveryCLI(context.Background(), "review", []string{"--job", "7", "--url", server.URL}, &out); err == nil || calls.Load() != 0 {
		t.Fatal(err, calls.Load())
	}
	if err := recoveryCLI(context.Background(), "review", []string{"--job", "7", "--action", "retry", "--confirm-no-delivery", "--url", server.URL}, &out); err != nil || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer destination.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer redirector.Close()
	if _, err := reviewRequest(context.Background(), redirector.URL, secret, "GET", "/v1/topic-review?job=7", nil); err == nil || strings.Contains(err.Error(), secret) || redirected.Load() != 0 {
		t.Fatal(err, redirected.Load())
	}
}

func TestRecoveryCLIHelpNeedsNoDatabaseOrCredentials(t *testing.T) {
	t.Setenv("BRIDGE_ADMIN_TOKEN", "")
	var out bytes.Buffer
	if err := recoveryCLI(context.Background(), "reconcile-topic", []string{"--help"}, &out); err != nil || !strings.Contains(out.String(), "--file") {
		t.Fatal(out.String(), err)
	}
}
