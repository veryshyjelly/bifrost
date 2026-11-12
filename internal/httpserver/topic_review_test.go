package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/recovery"
)

type topicStoreFixture struct {
	fixtureStore
	reads, writes int
	err           error
}

func (s *topicStoreFixture) InspectTopic(_ context.Context, id int64) (recovery.TopicSnapshot, error) {
	s.reads++
	return recovery.TopicSnapshot{AccountID: "account", Job: id, State: "uncertain", Effect: "frozen:topic", EffectState: "uncertain", RequestDigest: strings.Repeat("a", 64), Management: -100, Name: "Original", Scope: "status"}, s.err
}
func (s *topicStoreFixture) ReconcileTopic(_ context.Context, d recovery.TopicDecision) (recovery.TopicResult, error) {
	s.writes++
	return recovery.TopicResult{Job: d.Job, Effect: d.Effect, Action: d.Action, ThreadID: d.ThreadID, State: "pending"}, s.err
}

func TestTopicReviewHTTPRequiresAuthStrictEvidenceAndOneObject(t *testing.T) {
	s := &topicStoreFixture{}
	secret := strings.Repeat("x", 32)
	handler := Handler(s, secret)
	d := recovery.TopicDecision{OperationID: "review", AccountID: "account", Job: 7, Effect: "frozen:topic", ExpectedState: "uncertain", ExpectedEffectState: "uncertain", RequestDigest: strings.Repeat("a", 64), Management: -100, Action: "adopt", ThreadID: 99, ConfirmCreated: true}
	valid, _ := json.Marshal(d)
	for _, tc := range []struct {
		auth, body string
		status     int
	}{{"", string(valid), 401}, {"Bearer " + secret, `{}`, 400}, {"Bearer " + secret, string(valid) + ` {}`, 400}, {"Bearer " + secret, strings.TrimSuffix(string(valid), "}") + `,"unexpected":true}`, 400}, {"Bearer " + secret, strings.Replace(string(valid), `"confirmCreated":true`, `"confirmCreated":false`, 1), 400}, {"Bearer " + secret, string(valid), 200}} {
		r := httptest.NewRequest("POST", "/v1/topic-review", strings.NewReader(tc.body))
		r.Header.Set("Authorization", tc.auth)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(w.Code, tc.status, w.Body.String())
		}
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("secret exposed")
		}
	}
	if s.writes != 1 {
		t.Fatal("invalid input reached reconciliation", s.writes)
	}
	d.AccountID = strings.Repeat("a", 17<<10)
	large, _ := json.Marshal(d)
	r := httptest.NewRequest("POST", "/v1/topic-review", strings.NewReader(string(large)))
	r.Header.Set("Authorization", "Bearer "+secret)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 413 || s.writes != 1 {
		t.Fatal(w.Code, s.writes)
	}
}

func TestTopicReviewHTTPInspectionAndConflictErrorsStaySafe(t *testing.T) {
	s := &topicStoreFixture{}
	secret := strings.Repeat("x", 32)
	handler := Handler(s, secret)
	for _, tc := range []struct {
		path, auth string
		status     int
	}{{"/v1/topic-review?job=7", "", 401}, {"/v1/topic-review?job=7&job=8", "Bearer " + secret, 400}, {"/v1/topic-review?job=-1", "Bearer " + secret, 400}, {"/v1/topic-review?job=7", "Bearer " + secret, 200}} {
		r := httptest.NewRequest("GET", tc.path, nil)
		r.Header.Set("Authorization", tc.auth)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if s.reads != 1 {
		t.Fatal(s.reads)
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{recovery.ErrConflict, 409}, {errors.New("private credentials must not be exposed"), 503}} {
		s.err = tc.err
		r := httptest.NewRequest("GET", "/v1/topic-review?job=7", nil)
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status || strings.Contains(w.Body.String(), "private credentials") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
