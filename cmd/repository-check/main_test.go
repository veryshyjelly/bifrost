package main

import (
	"strings"
	"testing"
)

func TestForbiddenPaths(t *testing.T) {
	for _, name := range []string{"bridge.env", ".env", ".env.local", ".go.env", "data/state", "backups/archive", "state.db-wal", "old.sqlite", "x.bifrost.tar.gz", ".bifrost-import-x/file", "vendor/source.go", "media/sticker.webp", "openspec/specs/media/sticker.webp", "backups/openspec/specs/media/spec.md"} {
		if !forbiddenPath(name) {
			t.Errorf("private path allowed: %s", name)
		}
	}
	for _, name := range []string{".go.env.example", "docs/bridge.env.example", "internal/postgres/doc.go", "testdata/message.json", "openspec/specs/media/spec.md", "openspec/changes/cache-sticker-conversions/specs/media/spec.md"} {
		if forbiddenPath(name) {
			t.Errorf("source/example path refused: %s", name)
		}
	}
	if !forbiddenPath("openspec/changes/cache-sticker-conversions/specs/media/sticker.webp") {
		t.Fatal("runtime media allowed beside the change contract")
	}
}

func TestCredentialDetection(t *testing.T) {
	secret := []byte(strings.Repeat("private-value", 3))
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"provider token", "123456" + ":" + strings.Repeat("a", 32), true},
		{"private key", "-----BEGIN " + "RSA PRIVATE KEY-----", true},
		{"operator secret", "value=" + string(secret), true},
		{"configuration name", "TELEGRAM_MASTER_TOKEN is required", false},
		{"empty input", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasCredential([]byte(tc.content), [][]byte{secret, nil}); got != tc.want {
				t.Fatalf("credential detection = %v, want %v", got, tc.want)
			}
		})
	}
}
