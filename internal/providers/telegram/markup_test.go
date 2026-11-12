package telegram

import (
	"fmt"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeMarkupRetainsScopedChoiceAndEmptyKeyboard(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		if !strings.HasSuffix(r.URL.Path, "editMessageReplyMarkup") || r.FormValue("chat_id") != "-100" || r.FormValue("message_id") != "101" {
			t.Error("markup target changed")
		}
		if calls == 1 && !strings.Contains(r.FormValue("reply_markup"), "rev-keep:task") {
			t.Error("native scoped choice missing")
		}
		if calls == 2 && r.FormValue("reply_markup") != `{"inline_keyboard":[]}` {
			t.Error("empty native keyboard changed", r.FormValue("reply_markup"))
		}
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":101,"date":1700000000,"chat":{"id":-100,"type":"supergroup"}}}`)
	}))
	defer server.Close()
	c, err := New(server.URL, "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, markup := range []*models.InlineKeyboardMarkup{{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: "Keep", CallbackData: "rev-keep:task"}}}}, {InlineKeyboard: [][]models.InlineKeyboardButton{}}} {
		if err := c.EditMarkup(t.Context(), &bot.EditMessageReplyMarkupParams{ChatID: int64(-100), MessageID: 101, ReplyMarkup: markup}); err != nil {
			t.Fatal(err)
		}
	}
}
