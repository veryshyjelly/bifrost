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

func TestNativePollVoteClearsWithArrayAndPreservesPollMessageField(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var p map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Error(err)
		}
		if !strings.HasSuffix(r.URL.Path, "/vote-poll") || string(p["pollMessageId"]) != `"source-poll"` || string(p["options"]) != `[]` || p["messageId"] != nil {
			t.Error("wrong native poll payload", p)
		}
		fmt.Fprint(w, `{"success":true}`)
	}))
	defer server.Close()
	c, err := New(server.URL, "fixture-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.VotePoll(t.Context(), "fixture-session", sdk.VotePollRequest{ChatID: "peer@g.us", PollMessageID: "source-poll"}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("SDK repeated a vote", calls)
	}
}
func TestNativePollVoteTransportFailureIsRetryableSetOperation(t *testing.T) {
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
	err = c.VotePoll(t.Context(), "fixture-session", sdk.VotePollRequest{ChatID: "peer@g.us", PollMessageID: "source-poll", Options: []string{" A "}})
	var outcome *providers.Error
	if !errors.As(err, &outcome) || !outcome.Retry || outcome.Uncertain {
		t.Fatal(err)
	}
}

func TestNativePollVoteUnsupportedEngineIsDefinitive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(501)
		fmt.Fprint(w, `{"message":"fixture unsupported"}`)
	}))
	defer server.Close()
	c, err := New(server.URL, "fixture-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = c.VotePoll(t.Context(), "fixture-session", sdk.VotePollRequest{ChatID: "peer@g.us", PollMessageID: "source-poll", Options: []string{"A"}})
	var outcome *providers.Error
	if !errors.As(err, &outcome) || outcome.Retry || outcome.Uncertain || outcome.Status != 501 {
		t.Fatal(err)
	}
}
