package telegram

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func TestNativeMenuSettingsKeepScopeAndSafeOutcomes(t *testing.T) {
	for _, status := range []int{200, 400, 403, 429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
				}
				if calls == 1 {
					if r.FormValue("scope") != `{"type":"chat_member","chat_id":-1001234567890123,"user_id":9007199254740993}` {
						t.Error("native scope changed", r.FormValue("scope"))
					}
				} else if r.FormValue("menu_button") != `{"type":"commands"}` || r.FormValue("chat_id") != "" {
					t.Error("default commands menu changed")
				}
				if status == 200 {
					fmt.Fprint(w, `{"ok":true,"result":true}`)
				} else {
					fmt.Fprintf(w, `{"ok":false,"error_code":%d,"description":"private error"}`, status)
				}
			}))
			defer s.Close()
			client, err := New(s.URL, "fixture", nil)
			if err != nil {
				t.Fatal(err)
			}
			errs := []error{client.SetMyCommands(t.Context(), &bot.SetMyCommandsParams{Commands: []models.BotCommand{{Command: "start", Description: "Help"}}, Scope: &models.BotCommandScopeChatMember{ChatID: int64(-1001234567890123), UserID: 9007199254740993}}), client.SetChatMenuButton(t.Context(), &bot.SetChatMenuButtonParams{MenuButton: &models.MenuButtonCommands{Type: models.MenuButtonTypeCommands}})}
			for _, err := range errs {
				if status == 200 {
					if err != nil {
						t.Fatal(err)
					}
				} else {
					var e *providers.Error
					if !errors.As(err, &e) || e.Status != status || e.Uncertain || e.Retry != (status == 429 || status == 503) {
						t.Fatal(err)
					}
				}
			}
			if calls != 2 {
				t.Fatal("SDK retried settings invisibly", calls)
			}
		})
	}
}
