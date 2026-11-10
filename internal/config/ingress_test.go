package config

import (
	"strings"
	"testing"
)

func TestIngressIsExplicitAndFailsClosedOnIncompleteProviderIdentity(t *testing.T) {
	values := map[string]string{"DATABASE_URL": "postgres://fixture@localhost/bifrost", "BRIDGE_ADMIN_TOKEN": strings.Repeat("a", 32), "BRIDGE_ENCRYPTION_KEY": strings.Repeat("ab", 32)}
	get := func(k string) string { return values[k] }
	c, err := Load(get)
	if err != nil || c.IngressEnabled {
		t.Fatal("diagnostic mode", err)
	}
	values["BRIDGE_INGRESS_ENABLED"] = "true"
	if _, err := Load(get); err == nil {
		t.Fatal("incomplete ingress enabled")
	}
	values["TELEGRAM_OWNER_ID"] = "123"
	values["TELEGRAM_MANAGEMENT_CHAT_ID"] = "-100"
	values["TELEGRAM_MASTER_TOKEN"] = "999:private-fixture-token"
	values["OPENWA_SESSION_ID"] = "current"
	values["OPENWA_WEBHOOK_SECRET"] = strings.Repeat("w", 32)
	c, err = Load(get)
	if err != nil || !c.IngressEnabled || c.MasterBotID != 999 {
		t.Fatal(c.IngressEnabled, err)
	}
	for key, bad := range map[string]string{"BRIDGE_INGRESS_ENABLED": "maybe", "TELEGRAM_OWNER_ID": "9007199254740992", "TELEGRAM_MANAGEMENT_CHAT_ID": "123", "TELEGRAM_MASTER_TOKEN": "private-fixture-token", "OPENWA_SESSION_ID": "", "OPENWA_WEBHOOK_SECRET": "short-private-key"} {
		old := values[key]
		values[key] = bad
		_, err := Load(get)
		if err == nil {
			t.Fatal("accepted invalid setting", key)
		}
		if bad != "" && strings.Contains(err.Error(), bad) {
			t.Fatal("private setting exposed")
		}
		values[key] = old
	}
}
