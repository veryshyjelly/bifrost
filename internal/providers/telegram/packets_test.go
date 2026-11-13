package telegram

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

func TestTextPacketOptionalMarkupOnWire(t *testing.T) {
	for _, tc := range []struct {
		name   string
		markup *models.InlineKeyboardMarkup
		want   string
	}{
		{name: "absent"},
		{name: "empty", markup: &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}}, want: `{"inline_keyboard":[]}`},
		{name: "choice", markup: &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: "Keep", CallbackData: "rev-keep:fixture"}}}}, want: `{"inline_keyboard":[[{"text":"Keep","callback_data":"rev-keep:fixture"}]]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
				}
				if got := r.FormValue("reply_markup"); got != tc.want {
					t.Errorf("reply_markup = %q, want %q", got, tc.want)
				}
				if tc.markup == nil && r.MultipartForm.Value["reply_markup"] != nil {
					t.Error("absent markup must be omitted")
				}
				if r.FormValue("message_thread_id") != "540" || r.FormValue("text") != "fixture" {
					t.Error("packet destination or text changed")
				}
				fmt.Fprint(w, `{"ok":true,"result":{"message_id":101,"date":1700000000,"chat":{"id":-100,"type":"supergroup"}}}`)
			}))
			defer server.Close()
			client, err := New(server.URL, "fixture", nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.SendPacket(t.Context(), relay.NativePacket{Kind: "text", Target: control.Target{BotID: "master", ChatID: -100, ThreadID: 540}, Body: presentation.Value{Text: "fixture"}, Markup: tc.markup}, nil)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
