// Package config loads private runtime settings without exposing their values in errors.
package config

import (
	"encoding/hex"
	"errors"
	"net/url"
	"strconv"
)

// Config contains the settings needed by the persistence and diagnostic runtime.
// Provider configuration is added with the provider adapters.
type Config struct {
	DatabaseURL   string
	EncryptionKey [32]byte
	AdminToken    string
	AccountID     string
	Port          int
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
	return c, nil
}
