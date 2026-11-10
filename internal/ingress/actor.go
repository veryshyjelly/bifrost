package ingress

import (
	"encoding/json"
	"github.com/go-telegram/bot/models"
)

// AuthorizedUpdate verifies the native actor against the exact JSON keys as
// well as the configured owner. encoding/json alone accepts case aliases.
func AuthorizedUpdate(raw json.RawMessage, update *models.Update, ownerID int64) bool {
	owner := actor(update)
	body := object(raw)
	var sourceActor map[string]json.RawMessage
	for _, source := range []struct{ update, key string }{{"callback_query", "from"}, {"message", "from"}, {"edited_message", "from"}, {"message_reaction", "user"}, {"my_chat_member", "from"}, {"managed_bot", "user"}, {"poll_answer", "user"}} {
		if event := object(body[source.update]); event != nil {
			sourceActor = object(event[source.key])
			break
		}
	}
	id, ok := integer(sourceActor["id"])
	var isBot bool
	if json.Unmarshal(sourceActor["is_bot"], &isBot) != nil {
		isBot = false
	}
	return owner != nil && ok && id == owner.ID && !isBot && !owner.IsBot && owner.ID == ownerID
}
