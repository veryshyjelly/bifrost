package config

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRuntimeConfigurationPreservesDefaultsAndRequiresExplicitIdentity(t *testing.T) {
	env := map[string]string{"DATABASE_URL": "postgres://fixture@localhost/bifrost", "BRIDGE_ADMIN_TOKEN": strings.Repeat("a", 32), "BRIDGE_ENCRYPTION_KEY": strings.Repeat("ab", 32), "BIFROST_ACCOUNT_ID": "original-account", "OPENWA_SESSION_ID": "session", "OPENWA_WEBHOOK_SECRET": strings.Repeat("b", 32), "OPENWA_API_KEY": "private-api-key", "TELEGRAM_OWNER_ID": "123", "TELEGRAM_MANAGEMENT_CHAT_ID": "-100", "TELEGRAM_MASTER_TOKEN": "999:private-master-token"}
	get := func(k string) string { return env[k] }
	c, err := LoadRuntime(get)
	if err != nil || !c.IngressEnabled || c.HistoryLimit != 20 || c.MaxBytes != 100<<20 || c.DeliveryInterval != 1100*time.Millisecond {
		t.Fatal("runtime defaults changed", err)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", c, c), "private-api-key") || strings.Contains(fmt.Sprintf("%+v", c), "private-master-token") {
		t.Fatal("runtime formatting exposed credentials")
	}
	for _, setting := range []struct{ key, value string }{{"BIFROST_ACCOUNT_ID", ""}, {"OPENWA_API_KEY", ""}, {"OPENWA_URL", "private-invalid-url"}, {"MAX_MEDIA_BYTES", "-1"}, {"HISTORY_LIMIT", "2001"}, {"DELIVERY_INTERVAL_MS", "99"}, {"STICKER_CHROMIUM_NO_SANDBOX", "private-invalid-bool"}} {
		old := env[setting.key]
		env[setting.key] = setting.value
		if _, err := LoadRuntime(get); err == nil || strings.Contains(err.Error(), "private-invalid") {
			t.Fatal("invalid configuration accepted or leaked", setting.key, err)
		}
		env[setting.key] = old
	}
}
