// Package retained reads immutable state needed by the current runtime.
// It validates saved configuration without executing environment-file content
// and preserves established identity keys used by imported delivery evidence.
package retained

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/veryshyjelly/bifrost/internal/bots"
)

var ErrInvalid = errors.New("invalid retained account state")

// Configuration holds validated private settings and copies of their original
// bytes. Its formatting methods redact values; explicit accessors expose secrets.
type Configuration struct {
	bridge, telegram          map[string]string
	bridgeRaw, telegramRaw    []byte
	key                       [32]byte
	master, owner, management int64
}

func (Configuration) String() string { return "retained configuration (private)" }

func (Configuration) GoString() string { return "retained.Configuration{private}" }

func (c Configuration) Value(name string) string { return c.bridge[name] }

func (c Configuration) EncryptionKey() [32]byte { return c.key }

func (c Configuration) MasterID() int64 { return c.master }

func (c Configuration) OwnerID() int64 { return c.owner }

func (c Configuration) ManagementChatID() int64 { return c.management }

// ParseConfiguration checks literal environment entries, matching Telegram API
// credentials and permanent owner/master identity fields. It does not source a
// shell file, expand variables or repair inconsistent saved values.
func ParseConfiguration(bridge, telegram []byte) (Configuration, error) {
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
