package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigureCreatesPrivateIndependentSecretsAndNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.env")
	if err := Configure(path); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(before), "\n") {
		_, value, ok := strings.Cut(line, "=")
		if !ok || len(value) != 64 {
			continue
		}
		if seen[value] {
			t.Fatal("independent secrets reused")
		}
		seen[value] = true
	}
	if len(seen) != 8 {
		t.Fatalf("generated %d secrets/identity values", len(seen))
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("private environment permissions")
	}
	if err := Configure(path); !errors.Is(err, os.ErrExist) {
		t.Fatal("existing configuration not refused")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("configuration rotated existing keys")
	}
}
