package config

import (
	"strings"
	"testing"
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
	if err != nil || c.Port != 3210 {
		t.Fatalf("valid settings: port=%d error=%v", c.Port, err)
	}
}
