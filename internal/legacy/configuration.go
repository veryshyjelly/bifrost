package legacy

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/veryshyjelly/bifrost/internal/bots"
	"github.com/veryshyjelly/bifrost/internal/secrets"
)

// Configuration preserves the original literal export values and file bytes.
// Its formatting methods deliberately omit credentials, including %#v output.
// No environment expansion, shell evaluation or archived code is involved.
type Configuration struct {
	bridge, telegram          map[string]string
	bridgeRaw, telegramRaw    []byte
	key                       [32]byte
	master, owner, management int64
}

func (Configuration) String() string             { return "legacy configuration (private)" }
func (Configuration) GoString() string           { return "legacy.Configuration{private}" }
func (c Configuration) Value(name string) string { return c.bridge[name] }
func (c Configuration) EncryptionKey() [32]byte  { return c.key }
func (c Configuration) MasterID() int64          { return c.master }
func (c Configuration) OwnerID() int64           { return c.owner }
func (c Configuration) ManagementChatID() int64  { return c.management }
func (c Configuration) Files() (bridge, telegram []byte) {
	return bytes.Clone(c.bridgeRaw), bytes.Clone(c.telegramRaw)
}
func (c Configuration) clone() Configuration {
	copyMap := func(source map[string]string) map[string]string {
		dest := make(map[string]string, len(source))
		for k, v := range source {
			dest[k] = v
		}
		return dest
	}
	c.bridge, c.telegram = copyMap(c.bridge), copyMap(c.telegram)
	c.bridgeRaw, c.telegramRaw = bytes.Clone(c.bridgeRaw), bytes.Clone(c.telegramRaw)
	return c
}

func parseConfiguration(bridge, telegram []byte) (Configuration, error) {
	c := Configuration{bridgeRaw: bytes.Clone(bridge), telegramRaw: bytes.Clone(telegram)}
	var err error
	c.bridge, err = literalEnvironment(bridge)
	if err != nil {
		return Configuration{}, err
	}
	c.telegram, err = literalEnvironment(telegram)
	if err != nil {
		return Configuration{}, err
	}
	key, err := hex.DecodeString(c.bridge["BRIDGE_ENCRYPTION_KEY"])
	if err != nil || len(key) != 32 {
		return Configuration{}, ErrInvalid
	}
	copy(c.key[:], key)
	for _, name := range []string{"TELEGRAM_API_ID", "TELEGRAM_API_HASH"} {
		if c.bridge[name] == "" || c.bridge[name] != c.telegram[name] {
			return Configuration{}, ErrInvalid
		}
	}
	apiID, err := strconv.ParseInt(c.bridge["TELEGRAM_API_ID"], 10, 32)
	if err != nil || apiID <= 0 {
		return Configuration{}, ErrInvalid
	}
	apiHash, err := hex.DecodeString(c.bridge["TELEGRAM_API_HASH"])
	if err != nil || len(apiHash) != 16 {
		return Configuration{}, ErrInvalid
	}
	c.owner, err = strconv.ParseInt(c.bridge["TELEGRAM_OWNER_ID"], 10, 64)
	if err != nil || c.owner <= 0 || c.owner > 9007199254740991 {
		return Configuration{}, ErrInvalid
	}
	c.management, err = strconv.ParseInt(c.bridge["TELEGRAM_MANAGEMENT_CHAT_ID"], 10, 64)
	if err != nil || c.management >= 0 || c.management < -9007199254740991 {
		return Configuration{}, ErrInvalid
	}
	c.master, err = strconv.ParseInt(strings.SplitN(c.bridge["TELEGRAM_MASTER_TOKEN"], ":", 2)[0], 10, 64)
	if err != nil || c.master <= 0 || c.master > 9007199254740991 || !bots.ValidToken(c.bridge["TELEGRAM_MASTER_TOKEN"], c.master) {
		return Configuration{}, ErrInvalid
	}
	for _, name := range []string{"BRIDGE_ADMIN_TOKEN", "OPENWA_WEBHOOK_SECRET"} {
		if len(c.bridge[name]) < 32 {
			return Configuration{}, ErrInvalid
		}
	}
	return c, nil
}

// Legacy transfer.ts stores unquoted literal NAME=value lines. Ambiguous
// duplicates are rejected instead of guessing which secret should win. No
// shell-style syntax is interpreted. All original bytes are kept.
func literalEnvironment(raw []byte) (map[string]string, error) {
	if len(raw) == 0 || len(raw) > 1<<20 || !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return nil, ErrInvalid
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok || name == "" {
			return nil, ErrInvalid
		}
		for i, r := range name {
			if r != '_' && (r < 'A' || r > 'Z') && (i == 0 || r < '0' || r > '9') {
				return nil, ErrInvalid
			}
		}
		if _, exists := values[name]; exists {
			return nil, ErrInvalid
		}
		values[name] = value
	}
	return values, nil
}

func validAccount(id string) bool {
	if id == "" || len(id) > 256 || !utf8.ValidString(id) {
		return false
	}
	for _, r := range id {
		if r <= 32 || r == 127 {
			return false
		}
	}
	return true
}

func (c Configuration) account(saved string) (string, error) {
	configured := c.bridge["BIFROST_ACCOUNT_ID"]
	if saved != "" && configured != "" && saved != configured {
		return "", ErrInvalid
	}
	id := saved
	if id == "" {
		id = configured
	}
	if id == "" {
		id = c.bridge["OPENWA_SESSION_ID"]
	}
	if !validAccount(id) {
		return "", ErrInvalid
	}
	return id, nil
}

func (c Configuration) validateBot(id string, raw []byte) error {
	var saved struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Ready    bool   `json:"ready"`
	}
	if decodeStrictJSON(raw, &saved) != nil {
		return ErrInvalid
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 || n > 9007199254740991 || strconv.FormatInt(n, 10) != id {
		return ErrInvalid
	}
	cipher, err := secrets.New(c.key)
	if err != nil {
		return ErrInvalid
	}
	token, err := cipher.Open(saved.Token)
	if err != nil || !bots.ValidToken(token, n) {
		return ErrInvalid
	}
	return nil
}

// decodeStrictJSON also rejects duplicate keys at every nesting level. The
// standard DisallowUnknownFields option alone does not detect those keys.
func decodeStrictJSON(raw []byte, dest any) error {
	if !utf8.Valid(raw) {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := uniqueJSONValue(d, 0); err != nil {
		return ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dest); err != nil {
		return ErrInvalid
	}
	return nil
}

func uniqueJSONValue(d *json.Decoder, depth int) error {
	if depth > 64 {
		return ErrInvalid
	}
	token, err := d.Token()
	if err != nil {
		return ErrInvalid
	}
	if value, ok := token.(string); ok && strings.ContainsRune(value, 0) {
		return ErrInvalid
	}
	switch token {
	case json.Delim('{'):
		keys := make(map[string]bool)
		for d.More() {
			t, err := d.Token()
			key, ok := t.(string)
			if err != nil || !ok || keys[key] {
				return ErrInvalid
			}
			keys[key] = true
			if uniqueJSONValue(d, depth+1) != nil {
				return ErrInvalid
			}
		}
		if t, err := d.Token(); err != nil || t != json.Delim('}') {
			return ErrInvalid
		}
	case json.Delim('['):
		for d.More() {
			if uniqueJSONValue(d, depth+1) != nil {
				return ErrInvalid
			}
		}
		if t, err := d.Token(); err != nil || t != json.Delim(']') {
			return ErrInvalid
		}
	default:
		if _, ok := token.(json.Delim); ok {
			return ErrInvalid
		}
	}
	return nil
}

// Ensure the redacted formatting implementation remains available to callers.
var _ fmt.Stringer = Configuration{}
