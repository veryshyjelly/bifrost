package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadRejectsInvalidSettingsWithoutLeakingValues(t *testing.T) {
	valid := map[string]string{"DATABASE_URL": "postgres://fixture@localhost/bifrost", "BRIDGE_ADMIN_TOKEN": strings.Repeat("a", 32), "BRIDGE_ENCRYPTION_KEY": strings.Repeat("ab", 32)}
	cases := []struct{ name, value string }{
		{"DATABASE_URL", "not-a-database://private-credential@localhost/db"},
		{"DATABASE_URL", "postgres://localhost/"},
		{"BRIDGE_ADMIN_TOKEN", "short-private-token"},
		{"BRIDGE_ENCRYPTION_KEY", strings.Repeat("xz", 32)},
		{"BRIDGE_PORT", "65536"},
		{"BRIDGE_PORT", "NaN"},
		{"BRIDGE_BIND_HOST", "private-invalid-bind-host"},
		{"BRIDGE_BIND_HOST", "127.0.0.1:3210"},
		{"COMMAND_TTL_SECONDS", "0004"},
		{"CONTROL_TTL_SECONDS", "86401"},
		{"CONTROL_TTL_SECONDS", "private-invalid-duration"},
		{"SYNC_CRON", "private-invalid-expression"},
		{"SYNC_TIMEZONE", "private-invalid-timezone"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			get := func(k string) string {
				if k == tc.name {
					return tc.value
				}
				return valid[k]
			}
			_, err := Load(get)
			if err == nil {
				t.Fatal("invalid configuration accepted")
			}
			if strings.Contains(err.Error(), tc.value) {
				t.Fatal("private value leaked")
			}
		})
	}
	c, err := Load(func(k string) string { return valid[k] })
	if err != nil || c.Port != 3210 || c.CommandTTL != 60*time.Second || c.ControlTTL != 600*time.Second {
		t.Fatalf("valid settings: port=%d error=%v", c.Port, err)
	}
}

func TestLoadExplicitBindIPAndDefaultListener(t *testing.T) {
	for _, tc := range []struct{ host, address string }{
		{"", ":53210"},
		{"127.0.0.1", "127.0.0.1:53210"},
		{"::1", "[::1]:53210"},
	} {
		t.Run(tc.address, func(t *testing.T) {
			values := map[string]string{
				"DATABASE_URL":          "postgres://fixture@localhost/bifrost",
				"BRIDGE_ADMIN_TOKEN":    strings.Repeat("a", 32),
				"BRIDGE_ENCRYPTION_KEY": strings.Repeat("ab", 32),
				"BRIDGE_PORT":           "53210", "BRIDGE_BIND_HOST": tc.host,
			}
			c, err := Load(func(name string) string { return values[name] })
			if err != nil {
				t.Fatal(err)
			}
			if c.ListenAddress() != tc.address {
				t.Fatalf("listen address = %q; want %q", c.ListenAddress(), tc.address)
			}
		})
	}
}
