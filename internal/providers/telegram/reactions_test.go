package telegram

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/providers"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeReactionSetterAndCustomEmojiUseSDKPayloads(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		if strings.HasSuffix(r.URL.Path, "getCustomEmojiStickers") {
			if r.FormValue("custom_emoji_ids") != `["12345"]` {
				t.Error("custom emoji ID changed")
			}
			fmt.Fprint(w, `{"ok":true,"result":[{"file_id":"file","file_unique_id":"unique","type":"custom_emoji","width":100,"height":100,"is_animated":false,"is_video":false,"custom_emoji_id":"12345","emoji":"❤️"}]}`)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "setMessageReaction") || r.FormValue("chat_id") != "-100" || r.FormValue("message_id") != "101" {
			t.Error("native reaction target changed")
		}
		var reaction []models.ReactionType
		if value := r.FormValue("reaction"); value != "" {
			if err := json.Unmarshal([]byte(value), &reaction); err != nil {
				t.Error(err)
			}
		}
		if calls == 1 && (len(reaction) != 1 || reaction[0].ReactionTypeEmoji == nil || reaction[0].ReactionTypeEmoji.Emoji != "👍") {
			t.Error("native emoji changed")
		}
		if calls == 2 && len(reaction) != 0 {
			t.Error("native removal omitted")
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()
	c, err := New(server.URL, "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetReaction(t.Context(), &bot.SetMessageReactionParams{ChatID: int64(-100), MessageID: 101, Reaction: []models.ReactionType{{Type: models.ReactionTypeTypeEmoji, ReactionTypeEmoji: &models.ReactionTypeEmoji{Emoji: "👍"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetReaction(t.Context(), &bot.SetMessageReactionParams{ChatID: int64(-100), MessageID: 101, Reaction: []models.ReactionType{}}); err != nil {
		t.Fatal(err)
	}
	stickers, err := c.CustomEmoji(t.Context(), "12345")
	if err != nil || len(stickers) != 1 || stickers[0].Emoji != "❤️" {
		t.Fatal(stickers, err)
	}
	if calls != 3 {
		t.Fatal("SDK repeated a native operation", calls)
	}
}
func TestNativeReactionTransportInterruptionIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	}))
	defer server.Close()
	c, err := New(server.URL, "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = c.SetReaction(t.Context(), &bot.SetMessageReactionParams{ChatID: int64(-100), MessageID: 101, Reaction: []models.ReactionType{}})
	var outcome *providers.Error
	if !errors.As(err, &outcome) || !outcome.Retry || outcome.Uncertain {
		t.Fatal(err)
	}
}
