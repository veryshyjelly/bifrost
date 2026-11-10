package telegram

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func TestSDKUsesLocalAPIAndPreservesNativeTopicsRepliesAndEntities(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.HasSuffix(r.URL.Path, "getMe") {
			fmt.Fprint(w, `{"ok":true,"result":{"id":42,"is_bot":true,"first_name":"Fixture"}}`)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		if r.FormValue("chat_id") != "-1001" || r.FormValue("message_thread_id") != "7" || !strings.Contains(r.FormValue("reply_parameters"), `"message_id":9`) || !strings.Contains(r.FormValue("entities"), `"type":"bold"`) {
			t.Error("native Telegram fields lost")
		}
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":77,"chat":{"id":-1001,"type":"supergroup"},"date":1}}`)
	}))
	defer server.Close()
	client, err := New(server.URL, "private-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("constructing a client called getMe or installed a webhook")
	}
	if me, err := client.Me(t.Context()); err != nil || me.ID != 42 {
		t.Fatalf("getMe: %v", err)
	}
	if m, err := client.SendText(t.Context(), &bot.SendMessageParams{ChatID: "-1001", MessageThreadID: 7, Text: "hello", ReplyParameters: &models.ReplyParameters{MessageID: 9}, Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBold, Offset: 0, Length: 5}}}); err != nil || m.ID != 77 {
		t.Fatalf("send: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatal("unexpected SDK requests")
	}
}

func TestTelegramSDKErrorBodiesNeverEscapeAndNoSendRetries(t *testing.T) {
	for _, tc := range []struct {
		name, body       string
		status           int
		retry, uncertain bool
		reason           string
	}{
		{"rate", `{"ok":false,"error_code":429,"description":"private body","parameters":{"retry_after":4}}`, 200, true, false, ""},
		{"missing thread", `{"ok":false,"error_code":400,"description":"private body: message thread not found"}`, 200, false, false, "thread_missing"},
		{"server", `{"ok":false,"error_code":500,"description":"private body"}`, 200, false, true, ""},
		{"invalid success", `private body`, 200, false, true, ""},
		{"missing receipt", `{"ok":true,"result":{}}`, 200, false, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client, _ := New(server.URL, "private-token", nil)
			_, err := client.SendText(t.Context(), &bot.SendMessageParams{ChatID: 123, Text: "hello"})
			var outcome *providers.Error
			if !errors.As(err, &outcome) || outcome.Retry != tc.retry || outcome.Uncertain != tc.uncertain || outcome.Reason != tc.reason {
				t.Fatalf("outcome: %+v", err)
			}
			if tc.retry && outcome.RetryAfter != 4*time.Second {
				t.Fatal("retry_after not retained")
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatal("SDK error leaked provider data")
			}
			if calls.Load() != 1 {
				t.Fatal("send automatically retried")
			}
		})
	}
}
