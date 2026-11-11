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

func TestNativeDeletesAreBoundedAndFailuresAreSafeToRetry(t *testing.T) {
	for _, tc := range []struct {
		status int
		retry  bool
	}{{200, false}, {400, false}, {403, false}, {429, true}, {503, true}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
				}
				if r.FormValue("chat_id") != "-200" || r.FormValue("message_ids") != "[11,12]" {
					t.Error("delete targets changed")
				}
				if tc.status == 200 {
					fmt.Fprint(w, `{"ok":true,"result":true}`)
				} else {
					fmt.Fprintf(w, `{"ok":false,"error_code":%d,"description":"private provider description"}`, tc.status)
				}
			}))
			defer s.Close()
			client, err := New(s.URL, "fixture", nil)
			if err != nil {
				t.Fatal(err)
			}
			err = client.DeleteMessages(t.Context(), &bot.DeleteMessagesParams{ChatID: int64(-200), MessageIDs: []int{11, 12}})
			if tc.status == 200 {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var p *providers.Error
				if !errors.As(err, &p) || p.Status != tc.status || p.Retry != tc.retry || p.Uncertain {
					t.Fatal(err)
				}
			}
			if calls != 1 {
				t.Fatal("SDK retried deletion invisibly", calls)
			}
			if err := client.DeleteMessages(t.Context(), &bot.DeleteMessagesParams{ChatID: int64(-200), MessageIDs: make([]int, 101)}); !errors.Is(err, providers.ErrInvalidRequest) || calls != 1 {
				t.Fatal("oversize batch reached provider", err, calls)
			}
		})
	}
}
