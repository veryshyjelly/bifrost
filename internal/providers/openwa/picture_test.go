package openwa

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativePictureLookupsPreserveHiddenAndAvailablePictures(t *testing.T) {
	for _, id := range []string{"123@g.us", "12345000000001@lid"} {
		for _, body := range []string{`{"url":null}`, `{"url":"https://mmg.whatsapp.net/profile.jpg?token=fixture"}`} {
			t.Run(id+body, func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					expected := "/api/sessions/session/contacts/" + id + "/profile-picture"
					if strings.HasSuffix(id, "@g.us") {
						expected = "/api/sessions/session/groups/" + id + "/picture"
					}
					if r.Method != "GET" || r.URL.Path != expected {
						t.Error("unexpected source mutation/path", r.Method, r.URL.Path)
					}
					fmt.Fprint(w, body)
				}))
				defer server.Close()
				c, err := New(server.URL, "fixture-key", nil)
				if err != nil {
					t.Fatal(err)
				}
				url, err := c.PictureURL(t.Context(), "session", id)
				if err != nil || strings.Contains(body, "null") && url != nil || !strings.Contains(body, "null") && (url == nil || !strings.HasPrefix(*url, "https://mmg.whatsapp.net/")) {
					t.Fatal(url, err)
				}
				if calls != 1 {
					t.Fatal("hidden SDK retry", calls)
				}
			})
		}
	}
}
