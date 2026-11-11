package telegram

import (
	"fmt"
	"github.com/go-telegram/bot"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNativeChatInfoRetainsFullIDAndPrivacyFields(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		if r.FormValue("chat_id") != "-9007199254740993" {
			t.Error("chat ID changed")
		}
		fmt.Fprint(w, `{"ok":true,"result":{"id":-9007199254740993,"type":"supergroup","title":"Public","username":"primary","active_usernames":["primary","secondary"]}}`)
	}))
	defer s.Close()
	c, err := New(s.URL, "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	chat, err := c.Chat(t.Context(), &bot.GetChatParams{ChatID: int64(-9007199254740993)})
	if err != nil || chat.ID != -9007199254740993 || chat.Username != "primary" || len(chat.ActiveUsernames) != 2 {
		t.Fatal(chat, err)
	}
}
