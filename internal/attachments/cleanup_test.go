package attachments

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupRemovesOnlyAbandonedOwnedWork(t *testing.T) {
	a, err := NewAssets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	asset, err := a.Save(t.Context(), bytes.NewBufferString("frozen"), "file.bin", "application/octet-stream", 1024)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-25 * time.Hour)
	for _, name := range []string{".codec-old", ".codec-active", "legacy-private"} {
		if err := os.Mkdir(filepath.Join(a.root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{".incoming-old", ".incoming-active"} {
		if err := os.WriteFile(filepath.Join(a.root, name), []byte("partial"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(a.root, ".codec-symlink")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".codec-old", ".incoming-old", "legacy-private", asset.Hash} {
		if err := os.Chtimes(filepath.Join(a.root, name), old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Cleanup(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".codec-old", ".incoming-old"} {
		if _, err := os.Lstat(filepath.Join(a.root, name)); !os.IsNotExist(err) {
			t.Fatal("abandoned work retained", name, err)
		}
	}
	for _, name := range []string{".codec-active", ".incoming-active", "legacy-private", ".codec-symlink", asset.Hash} {
		if _, err := os.Lstat(filepath.Join(a.root, name)); err != nil {
			t.Fatal("retained object removed", name, err)
		}
	}
	f, err := a.Open(t.Context(), asset)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
}
