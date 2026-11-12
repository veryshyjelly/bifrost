// Package reactions maintains actor-specific native reactions and preserves
// unsupported custom-emoji selections without clearing the source reaction.
package reactions

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/graphemes"
	"github.com/veryshyjelly/bifrost/internal/presentation"
)

func EmojiKey(value string) string { return strings.ReplaceAll(value, "\uFE0F", "") }

// EmojiEquivalent validates the one-grapheme equivalent supplied by Telegram.
// It deliberately does not guess a standard emoji from a custom sticker ID.
func EmojiEquivalent(value string) bool {
	if value == "" || !utf8.ValidString(value) || presentation.Length(value) > 32 {
		return false
	}
	g := graphemes.FromString(value)
	if !g.Next() || g.Next() {
		return false
	}
	for _, r := range value {
		if r == 0x20E3 || r >= 0x1F1E6 && r <= 0x1F1FF {
			return true
		}
		index := sort.Search(len(extendedPictographic), func(i int) bool { return extendedPictographic[i][1] >= r })
		if index < len(extendedPictographic) && extendedPictographic[index][0] <= r {
			return true
		}
	}
	return false
}

var telegramEquivalents = map[string]string{
	"😂": "🤣", "😀": "😁", "😃": "😁", "😄": "😁", "😆": "😁",
	"😠": "😡", "😔": "😢", "😞": "😢", "😥": "😢",
	"😙": "😘", "😚": "😘", "🥳": "🎉",
}

func TelegramEmoji(value string) string {
	value = EmojiKey(value)
	if replacement, found := telegramEquivalents[value]; found {
		return replacement
	}
	return value
}
