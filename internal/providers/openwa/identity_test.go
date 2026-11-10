package openwa

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeIdentityReadsRejectMismatchedAndIncompleteEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, body   string
		phone, valid bool
	}{
		{"resolved phone", `{"contactId":"987654321@lid","phone":"12025550123"}`, true, true},
		{"unknown phone", `{"contactId":"987654321@lid","phone":null}`, true, true},
		{"different identity", `{"contactId":"555555@lid","phone":"12025550123"}`, true, false},
		{"display value", `{"contactId":"987654321@lid","phone":"+1 (202) 555-0123"}`, true, false},
		{"complete roster", `{"id":"123@g.us","name":"Group","participants":[{"id":"987654321@lid","number":"987654321"}]}`, false, true},
		{"empty roster", `{"id":"123@g.us","name":"Group","participants":[]}`, false, true},
		{"missing roster", `{"id":"123@g.us","name":"Group"}`, false, false},
		{"foreign group", `{"id":"456@g.us","name":"Group","participants":[]}`, false, false},
		{"invalid member", `{"id":"123@g.us","name":"Group","participants":[{"id":"bad@g.us"}]}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.Header.Get("X-API-Key") != "fixture-key" {
					t.Error("invalid native identity request")
				}
				if tc.phone && !strings.HasSuffix(r.URL.Path, "/contacts/987654321@lid/phone") {
					t.Error("wrong phone path")
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client, err := New(server.URL, "fixture-key", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.phone {
				_, err = client.Phone(t.Context(), "fixture-session", "987654321@lid")
			} else {
				_, err = client.Group(t.Context(), "fixture-session", "123@g.us")
			}
			if (err == nil) != tc.valid || calls != 1 {
				t.Fatal(err, calls)
			}
		})
	}
}
