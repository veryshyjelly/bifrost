package legacy

import (
	"encoding/json"
	"sort"
	"strconv"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/bots"
	"github.com/veryshyjelly/bifrost/internal/secrets"
)

// ImportedBot separates activation (/start) and proven hosted logout from
// local API migration eligibility. Cached identity is only source evidence;
// the native setup workflow must still call the configured local API.
type ImportedBot struct {
	Credential   bots.Credential
	Started      bool
	HostedLogout bool
}

// ProjectBots converts only the credential capability. It does not activate
// an import, call a provider or mark a native lifecycle as migrated. Original
// member ciphertext strings survive exactly; the legacy plaintext master
// credential receives a new seal with the original encryption key.
func ProjectBots(c Configuration, values []KV) ([]ImportedBot, error) {
	cipher, err := secrets.New(c.key)
	if err != nil || c.master <= 0 {
		return nil, ErrInvalid
	}
	registered := map[string]ImportedBot{}
	identities := map[string]models.User{}
	logouts := map[string]bool{}
	masterToken := c.Value("TELEGRAM_MASTER_TOKEN")
	masterSealed, err := cipher.Seal(masterToken)
	if err != nil {
		return nil, err
	}
	registered["master"] = ImportedBot{Credential: bots.Credential{BotID: "master", Ciphertext: masterSealed, Revision: bots.Digest(masterToken), Identity: models.User{ID: c.master, IsBot: true}, CreatedNotice: true}}
	seen := make(map[string]bool)
	for _, v := range values {
		if v.Kind != "bot" && v.Kind != "telegram-identity" && v.Kind != "migrated" {
			continue
		}
		key := v.Kind + ":" + v.ID
		if seen[key] || !validText(v.ID, 128) || validateJSON([]byte(v.Value), false) != nil {
			return nil, ErrInvalid
		}
		seen[key] = true
		switch v.Kind {
		case "bot":
			if c.validateBot(v.ID, []byte(v.Value)) != nil {
				return nil, ErrInvalid
			}
			var source struct {
				Token, Username string
				Ready           bool
			}
			if json.Unmarshal([]byte(v.Value), &source) != nil {
				return nil, ErrInvalid
			}
			token, err := cipher.Open(source.Token)
			if err != nil {
				return nil, ErrInvalid
			}
			id, _ := strconv.ParseInt(v.ID, 10, 64)
			registered[v.ID] = ImportedBot{Credential: bots.Credential{BotID: v.ID, Ciphertext: source.Token, Revision: bots.Digest(token), Identity: models.User{ID: id, IsBot: true, Username: source.Username}, CreatedNotice: true}, Started: source.Ready}
		case "telegram-identity":
			var native models.User
			if json.Unmarshal([]byte(v.Value), &native) != nil || !native.IsBot || native.ID <= 0 || (v.ID != "master" && strconv.FormatInt(native.ID, 10) != v.ID) || (v.ID == "master" && native.ID != c.master) {
				return nil, ErrInvalid
			}
			identities[v.ID] = native
		case "migrated":
			if v.Value != "true" {
				return nil, ErrInvalid
			}
			logouts[v.ID] = true
		}
	}
	for id := range logouts {
		if _, ok := registered[id]; !ok {
			return nil, ErrInvalid
		}
	}
	for id := range identities {
		if _, ok := registered[id]; !ok {
			return nil, ErrInvalid
		}
	}
	result := make([]ImportedBot, 0, len(registered))
	for id, value := range registered {
		if native, ok := identities[id]; ok {
			// The saved registration username remains the routing/name source;
			// cached identity may predate a renamed bot. GetMe refreshes it later.
			if value.Credential.Identity.Username != "" {
				native.Username = value.Credential.Identity.Username
			}
			value.Credential.Identity = native
		}
		value.HostedLogout = logouts[id]
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Credential.BotID < result[j].Credential.BotID })
	return result, nil
}
