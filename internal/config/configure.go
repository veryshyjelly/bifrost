package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

func randomHex() (string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

// Configure creates a private installation environment exclusively: an existing
// file is never replaced, preserving account identity and encryption material.
func Configure(path string) error {
	values := make([]string, 8)
	for i := range values {
		value, err := randomHex()
		if err != nil {
			return err
		}
		values[i] = value
	}
	content := fmt.Sprintf(`# Generated once. Do not rotate these values over an existing installation.
POSTGRES_ADMIN_PASSWORD=%s
POSTGRES_PASSWORD=%s
OPENWA_POSTGRES_PASSWORD=%s
OPENWA_API_KEY_PEPPER=%s
BRIDGE_ENCRYPTION_KEY=%s
BRIDGE_ADMIN_TOKEN=%s
OPENWA_WEBHOOK_SECRET=%s
BIFROST_ACCOUNT_ID=%s
# Obtain these from my.telegram.org before running make up.
TELEGRAM_API_ID=
TELEGRAM_API_HASH=
# Pair a WhatsApp session in the OpenWA dashboard and create a scoped API key.
OPENWA_SESSION_ID=
OPENWA_API_KEY=
TELEGRAM_MASTER_TOKEN=
TELEGRAM_OWNER_ID=
TELEGRAM_MANAGEMENT_CHAT_ID=
# Start passive; select run and enable ingress after configuring provider identities.
BIFROST_COMMAND=serve
BRIDGE_INGRESS_ENABLED=false
COMMAND_TTL_SECONDS=60
CONTROL_TTL_SECONDS=600
SYNC_CRON=0 * * * *
SYNC_TIMEZONE=Asia/Kolkata
# Optional local source override: OPENWA_SOURCE=../OpenWa
`, values[0], values[1], values[2], values[3], values[4], values[5], values[6], values[7])
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	return f.Close()
}
