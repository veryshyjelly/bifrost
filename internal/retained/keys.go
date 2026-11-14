package retained

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

func HistoryDestinationKey(account, chat, message, bot string, telegramChat, thread int64) string {
	scope := bot
	if telegramChat < 0 {
		scope = "*"
	}
	raw := stringTupleJSON(account, chat, strconv.FormatInt(telegramChat, 10))
	raw = append(raw[:len(raw)-1], ',')
	raw = strconv.AppendInt(raw, thread, 10)
	raw = append(raw, ',')
	raw = append(raw, stringTupleJSON(scope)[1:]...)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]) + ":" + message
}

func OutgoingReactionKey(account, bot, key string) string {
	return stringTupleKey(account, bot, key)
}

func StatusScope(account string) string {
	sum := sha256.Sum256([]byte(account))
	return hex.EncodeToString(sum[:])
}

func StatusSkipKey(account, id string) string { return stringTupleKey(StatusScope(account), id) }

func stringTupleKey(values ...string) string {
	sum := sha256.Sum256(stringTupleJSON(values...))
	return hex.EncodeToString(sum[:])
}

func stringTupleJSON(values ...string) []byte {
	const digits = "0123456789abcdef"
	raw := []byte{'['}
	for i, value := range values {
		if i > 0 {
			raw = append(raw, ',')
		}
		raw = append(raw, '"')
		for _, r := range value {
			switch r {
			case '"', '\\':
				raw = append(raw, '\\', byte(r))
			case '\b':
				raw = append(raw, '\\', 'b')
			case '\f':
				raw = append(raw, '\\', 'f')
			case '\n':
				raw = append(raw, '\\', 'n')
			case '\r':
				raw = append(raw, '\\', 'r')
			case '\t':
				raw = append(raw, '\\', 't')
			default:
				if r < 32 {
					raw = append(raw, '\\', 'u', '0', '0', digits[r>>4], digits[r&15])
				} else {
					raw = append(raw, string(r)...)
				}
			}
		}
		raw = append(raw, '"')
	}
	raw = append(raw, ']')
	return raw
}
