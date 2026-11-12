package openwa

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/providers"
)

func TestNativeStatusListRequiresArrayAndUsesOnlyReadEndpoint(t *testing.T) {
	for _, body := range []string{`{"statuses":[]}`, `{"statuses":null}`, `{}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/sessions/session/status" {
					t.Error("Status list used a mutation or unexpected endpoint", r.Method, r.URL.Path)
				}
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			c, err := New(server.URL, "fixture-key", nil)
			if err != nil {
				t.Fatal(err)
			}
			result, err := c.ListStatuses(context.Background(), "session")
			if body == `{"statuses":[]}` {
				if err != nil || result == nil || result.Statuses == nil {
					t.Fatal(result, err)
				}
			} else {
				var outcome *providers.Error
				if !errors.As(err, &outcome) || outcome.Uncertain || !outcome.Retry {
					t.Fatal(err)
				}
			}
		})
	}
}
