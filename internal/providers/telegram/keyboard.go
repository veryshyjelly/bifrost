package telegram

import (
	"encoding/json"
	"github.com/go-telegram/bot/models"
)

// SDK v1.28.0 declares ChatHasUsername as bool,omitempty, which cannot express
// Telegram's explicit false constraint. Keep native SDK types and transport,
// correcting only request-chat privacy serialization at the adapter boundary.
type privateKeyboard models.ReplyKeyboardMarkup

func (k privateKeyboard) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(models.ReplyKeyboardMarkup(k))
	if err != nil {
		return nil, err
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	var rows [][]map[string]json.RawMessage
	if err := json.Unmarshal(out["keyboard"], &rows); err != nil {
		return nil, err
	}
	for i, row := range k.Keyboard {
		for j, button := range row {
			if button.RequestChat == nil {
				continue
			}
			var request map[string]json.RawMessage
			if err := json.Unmarshal(rows[i][j]["request_chat"], &request); err != nil {
				return nil, err
			}
			request["chat_has_username"], err = json.Marshal(button.RequestChat.ChatHasUsername)
			if err != nil {
				return nil, err
			}
			rows[i][j]["request_chat"], err = json.Marshal(request)
			if err != nil {
				return nil, err
			}
		}
	}
	out["keyboard"], err = json.Marshal(rows)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}
