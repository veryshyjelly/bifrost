// Package config loads private runtime settings without exposing their values in errors.
package config

import (
	"encoding/hex"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Config contains the settings needed by the persistence and diagnostic runtime.
// Provider configuration is added with the provider adapters.
type Config struct {
	DatabaseURL                            string
	EncryptionKey                          [32]byte
	AdminToken                             string
	AccountID                              string
	Port                                   int
	IngressEnabled                         bool
	OpenWASession, WebhookSecret           string
	OwnerID, ManagementChatID, MasterBotID int64
}

func Load(getenv func(string) string) (Config, error) {
	c := Config{DatabaseURL: getenv("DATABASE_URL"), AdminToken: getenv("BRIDGE_ADMIN_TOKEN"), AccountID: getenv("BIFROST_ACCOUNT_ID"), Port: 3210}
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.Path == "" || u.Path == "/" {
		return Config{}, errors.New("DATABASE_URL must name a PostgreSQL database")
	}
	key, err := hex.DecodeString(getenv("BRIDGE_ENCRYPTION_KEY"))
	if err != nil || len(key) != 32 {
		return Config{}, errors.New("BRIDGE_ENCRYPTION_KEY must contain 32 bytes in hex")
	}
	copy(c.EncryptionKey[:], key)
	if len(c.AdminToken) < 32 {
		return Config{}, errors.New("BRIDGE_ADMIN_TOKEN must contain at least 32 characters")
	}
	if p := getenv("BRIDGE_PORT"); p != "" {
		c.Port, err = strconv.Atoi(p)
		if err != nil || c.Port < 1 || c.Port > 65535 {
			return Config{}, errors.New("BRIDGE_PORT must be between 1 and 65535")
		}
	}
	if flag := getenv("BRIDGE_INGRESS_ENABLED"); flag != "" {
		c.IngressEnabled, err = strconv.ParseBool(flag)
		if err != nil {
			return Config{}, errors.New("BRIDGE_INGRESS_ENABLED must be true or false")
		}
	}
	if c.IngressEnabled {
		c.OpenWASession, c.WebhookSecret = getenv("OPENWA_SESSION_ID"), getenv("OPENWA_WEBHOOK_SECRET")
		c.OwnerID, err = strconv.ParseInt(getenv("TELEGRAM_OWNER_ID"), 10, 64)
		if err != nil || c.OwnerID <= 0 || c.OwnerID > 9007199254740991 {
			return Config{}, errors.New("TELEGRAM_OWNER_ID must be a positive safe integer")
		}
		c.ManagementChatID, err = strconv.ParseInt(getenv("TELEGRAM_MANAGEMENT_CHAT_ID"), 10, 64)
		if err != nil || c.ManagementChatID >= 0 || c.ManagementChatID < -9007199254740991 {
			return Config{}, errors.New("TELEGRAM_MANAGEMENT_CHAT_ID must be a negative safe integer")
		}
		bot, token, ok := strings.Cut(getenv("TELEGRAM_MASTER_TOKEN"), ":")
		c.MasterBotID, err = strconv.ParseInt(bot, 10, 64)
		if !ok || token == "" || err != nil || c.MasterBotID <= 0 || c.MasterBotID > 9007199254740991 {
			return Config{}, errors.New("TELEGRAM_MASTER_TOKEN must identify the master bot")
		}
		if c.OpenWASession == "" || len(c.WebhookSecret) < 32 {
			return Config{}, errors.New("ingress requires OPENWA_SESSION_ID and a 32-character OPENWA_WEBHOOK_SECRET")
		}
	}
	return c, nil
}
