package reactions

import "testing"

func TestCustomEmojiEquivalentRequiresOneUnicodePictographGrapheme(t *testing.T) {
	for _, value := range []string{"👍", "👍🏽", "👩‍👩‍👧‍👦", "🇮🇳", "1️⃣", "❤️", "©️", "🫩"} {
		if !EmojiEquivalent(value) {
			t.Errorf("valid native equivalent rejected %q", value)
		}
	}
	for _, value := range []string{"", "custom-id", "text👍", "👍👎", "A", "Á", " ", "\xff", "\uFE0F", "1"} {
		if EmojiEquivalent(value) {
			t.Errorf("invalid native equivalent accepted %q", value)
		}
	}
	if EmojiKey("❤️") != "❤" || TelegramEmoji("😂") != "🤣" || TelegramEmoji("👍") != "👍" {
		t.Fatal("baseline emoji equivalence changed")
	}
}
