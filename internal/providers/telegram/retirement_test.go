package telegram

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func TestNativeTopicRetirementKeepsExactTargetAndSafeOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		retry      bool
	}{
		{"success", `{"ok":true,"result":true}`, 0, false},
		{"already missing", `{"ok":false,"error_code":400,"description":"Bad Request: message thread not found"}`, 0, false},
		{"other bad request", `{"ok":false,"error_code":400,"description":"private provider body"}`, 400, false},
		{"permissions", `{"ok":false,"error_code":403,"description":"private provider body"}`, 403, false},
		{"rate limit", `{"ok":false,"error_code":429,"parameters":{"retry_after":60}}`, 429, true},
		{"unavailable", `{"ok":false,"error_code":503}`, 503, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
				}
				if r.URL.Path != "/botfixture/deleteForumTopic" || r.FormValue("chat_id") != "-1001234567890123" || r.FormValue("message_thread_id") != "2147483649" {
					t.Error("topic request changed")
				}
				fmt.Fprint(w, tc.body)
			}))
			defer s.Close()
			client, err := New(s.URL, "fixture", nil)
			if err != nil {
				t.Fatal(err)
			}
			err = client.DeleteForumTopic(t.Context(), &bot.DeleteForumTopicParams{ChatID: int64(-1001234567890123), MessageThreadID: 2147483649})
			if tc.status == 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var p *providers.Error
				if !errors.As(err, &p) || p.Status != tc.status || p.Retry != tc.retry || p.Uncertain {
					t.Fatal(err)
				}
			}
			for _, p := range []*bot.DeleteForumTopicParams{nil, {ChatID: int64(-100), MessageThreadID: 1}, {ChatID: int64(-100), MessageThreadID: 0}, {ChatID: int64(123), MessageThreadID: 44}} {
				if err := client.DeleteForumTopic(t.Context(), p); !errors.Is(err, providers.ErrInvalidRequest) {
					t.Fatal(err)
				}
			}
			if calls != 1 {
				t.Fatal("invalid target or hidden retry reached provider", calls)
			}
		})
	}
}
