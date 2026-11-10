package httpserver

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/delivery"
)

type fixtureStore struct {
	reviews int
	confirm bool
}

func (s *fixtureStore) Counts(context.Context) (map[string]int64, error) {
	return map[string]int64{"pending": 2}, nil
}
func (s *fixtureStore) Review(_ context.Context, _ int64, retry, confirm bool) error {
	s.reviews++
	s.confirm = confirm
	if retry && !confirm {
		return delivery.ErrUncertain
	}
	return nil
}
func TestReviewRequiresAdminAndExplicitConfirmation(t *testing.T) {
	store := &fixtureStore{}
	secret := strings.Repeat("a", 32)
	handler := Handler(store, secret)
	for _, tc := range []struct {
		auth, body string
		status     int
	}{
		{"", `{"job":1,"action":"retry","confirmNoDelivery":true}`, 401},
		{"Bearer " + secret, `{"job":1,"action":"retry"}`, 409},
		{"Bearer " + secret, `{"job":1,"action":"retry","confirmNoDelivery":true}`, 200},
		{"Bearer " + secret, `{"job":0,"action":"retry"}`, 400},
		{"Bearer " + secret, `[]`, 400},
	} {
		r := httptest.NewRequest("POST", "/v1/review", strings.NewReader(tc.body))
		r.Header.Set("Authorization", tc.auth)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("status=%d want=%d", w.Code, tc.status)
		}
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("admin token leaked")
		}
	}
	if store.reviews != 2 || !store.confirm {
		t.Fatal("invalid request reached store")
	}
}

func TestDiagnosticsDoNotClaimProviderReadiness(t *testing.T) {
	h := Handler(&fixtureStore{}, strings.Repeat("a", 32))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Fatal("incorrect diagnostic readiness")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/v1/status", nil))
	if w.Code != 401 {
		t.Fatal("unauthorized status exposed")
	}
}
