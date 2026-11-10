package telegram

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func TestNativeTopicAndMembershipFieldsPreserveFullWidthIDs(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		if r.FormValue("chat_id") != "-1001234567890" {
			t.Error("chat ID changed")
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "createForumTopic"):
			if r.FormValue("name") != "Fixture" || r.FormValue("icon_custom_emoji_id") != "emoji" {
				t.Error("topic fields lost")
			}
			fmt.Fprint(w, `{"ok":true,"result":{"message_thread_id":44,"name":"Fixture","icon_color":0}}`)
		case strings.HasSuffix(r.URL.Path, "getChatMember"):
			if r.FormValue("user_id") != "9007199254740991" {
				t.Error("member ID changed")
			}
			fmt.Fprint(w, `{"ok":true,"result":{"status":"member","user":{"id":9007199254740991,"is_bot":true,"first_name":"Fixture"}}}`)
		default:
			t.Error("unexpected provider call")
		}
	}))
	defer server.Close()
	client, err := New(server.URL, "private-fixture-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	topic, err := client.CreateForumTopic(t.Context(), &bot.CreateForumTopicParams{ChatID: int64(-1001234567890), Name: "Fixture", IconCustomEmojiID: "emoji"})
	if err != nil || topic.MessageThreadID != 44 {
		t.Fatal(topic, err)
	}
	member, err := client.ChatMember(t.Context(), &bot.GetChatMemberParams{ChatID: int64(-1001234567890), UserID: 9007199254740991})
	if err != nil || member.Type != models.ChatMemberTypeMember {
		t.Fatal(member, err)
	}
	if calls.Load() != 2 {
		t.Fatal("hidden startup or retry call", calls.Load())
	}
}
