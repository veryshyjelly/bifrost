package control

import (
	"encoding/json"
	"testing"
)

func TestMirrorEventPreservesLegacyStringAndNativeIntegerIDs(t *testing.T) {
	for _, raw := range []string{`{"type":"link_group","data":{"chatId":"group@g.us","tgChat":"-9007199254740993"}}`, `{"type":"link_group","data":{"chatId":"group@g.us","tgChat":-9007199254740993}}`} {
		var e MirrorEvent
		if err := json.Unmarshal([]byte(raw), &e); err != nil || e.Data.TelegramChat != -9007199254740993 {
			t.Fatal(e, err)
		}
	}
	for _, raw := range []string{`{"data":{"tgChat":"-1.5"}}`, `{"data":{"tgChat":-1.5}}`, `{"data":{"tgChat":"999999999999999999999999"}}`} {
		var e MirrorEvent
		if err := json.Unmarshal([]byte(raw), &e); err == nil {
			t.Fatal("invalid ID accepted", raw)
		}
	}
}
