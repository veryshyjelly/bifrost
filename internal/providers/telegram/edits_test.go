package telegram

import (
	"context"
	"fmt"
	"github.com/go-telegram/bot"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeEditMethodsKeepLiteralTextAndTreatNotModifiedAsComplete(t *testing.T) {
	for _, kind := range []string{"text", "caption", "not-modified"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var err error
				if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
					err = r.ParseMultipartForm(1 << 20)
				} else {
					err = r.ParseForm()
				}
				if err != nil {
					t.Error(err)
				}
				if r.FormValue("chat_id") != "-100" || r.FormValue("message_id") != "101" || r.FormValue("parse_mode") != "" {
					t.Error("edit lost native target or used parse mode")
				}
				if kind == "not-modified" {
					fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`)
					return
				}
				field := "text"
				method := "editMessageText"
				if kind == "caption" {
					field, method = "caption", "editMessageCaption"
				}
				if !strings.HasSuffix(r.URL.Path, method) || r.FormValue(field) != "<literal>" {
					t.Error("edit changed literal body or method")
				}
				fmt.Fprint(w, `{"ok":true,"result":{"message_id":101,"date":1700000000,"chat":{"id":-100,"type":"supergroup"}}}`)
			}))
			defer server.Close()
			c, err := New(server.URL, "fixture", nil)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "caption" {
				err = c.EditCaption(context.Background(), &bot.EditMessageCaptionParams{ChatID: int64(-100), MessageID: 101, Caption: "<literal>"})
			} else {
				err = c.EditText(context.Background(), &bot.EditMessageTextParams{ChatID: int64(-100), MessageID: 101, Text: "<literal>"})
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
