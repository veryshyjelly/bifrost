package legacy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoredAssetVerificationChecksBytesAndStructureWithoutWriting(t *testing.T) {
	root := t.TempDir()
	media := t.TempDir()
	telegram := t.TempDir()
	b := &Bundle{root: root, Account: "original-account", Digest: strings.Repeat("a", 64)}
	for _, dir := range []string{filepath.Join(root, "media"), filepath.Join(root, "telegram-data"), media, telegram} {
		if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "nested", "file"), []byte("frozen bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	receipt, err := VerifyRestoredAssets(t.Context(), b, media, telegram)
	if err != nil || receipt.Account != b.Account || receipt.Media.Files != 1 || receipt.Telegram.Bytes != 12 {
		t.Fatal("matching restore rejected", err)
	}
	if err := os.WriteFile(filepath.Join(media, "nested", "file"), []byte("changed bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyRestoredAssets(t.Context(), b, media, telegram); !errors.Is(err, ErrRestoredAssets) {
		t.Fatal("changed bytes accepted", err)
	}
	if err := os.WriteFile(filepath.Join(media, "nested", "file"), []byte("frozen bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(telegram, "nested", "file"), filepath.Join(media, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyRestoredAssets(t.Context(), b, media, telegram); !errors.Is(err, ErrRestoredAssets) {
		t.Fatal("symlink was accepted", err)
	}
	if err := os.Remove(filepath.Join(media, "link")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := VerifyRestoredAssets(ctx, b, media, telegram); !errors.Is(err, context.Canceled) {
		t.Fatal("asset scan ignored cancellation", err)
	}
}
