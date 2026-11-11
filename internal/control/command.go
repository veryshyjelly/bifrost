package control

import (
	"github.com/go-telegram/bot/models"
	"regexp"
	"strings"
	"unicode"
)

type ParsedCommand struct {
	Name, Recipient string
	Args            []string
}

var commandPattern = regexp.MustCompile(`^/([a-zA-Z0-9_]+)(?:@([a-zA-Z0-9_]+))?(?:[\s\v\p{Zs}\p{Zl}\p{Zp}\x{feff}]+([\s\S]*))?$`)

func jsSpace(r rune) bool { return r == 0xfeff || (r != 0x85 && unicode.IsSpace(r)) }
func ParseCommand(m *models.Message) (ParsedCommand, bool) {
	if m == nil {
		return ParsedCommand{}, false
	}
	text := m.Text
	if text == "" {
		text = m.Caption
	}
	match := commandPattern.FindStringSubmatch(strings.TrimFunc(text, jsSpace))
	if match == nil {
		return ParsedCommand{}, false
	}
	return ParsedCommand{Name: strings.ToLower(match[1]), Recipient: strings.ToLower(match[2]), Args: strings.FieldsFunc(match[3], jsSpace)}, true
}
