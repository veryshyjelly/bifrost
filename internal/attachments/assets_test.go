package attachments

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/providers"
)

func TestAssetsFreezeExactBytesAndRejectMissingOrChangedContent(t *testing.T) {
	a, err := NewAssets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("private fixture bytes")
	asset, err := a.Save(t.Context(), bytes.NewReader(data), "../attachment.bin", "application/octet-stream", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Name != "attachment.bin" || asset.Size != int64(len(data)) {
		t.Fatal(asset)
	}
	file, err := a.Open(t.Context(), asset)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	path, _ := a.Path(asset)
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, len(data)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Open(t.Context(), asset); !errors.Is(err, ErrAsset) {
		t.Fatal("changed bytes accepted", err)
	}
	if _, err := a.Save(t.Context(), bytes.NewReader(data), "attachment.bin", "application/octet-stream", 1024); !errors.Is(err, ErrAsset) {
		t.Fatal("corrupt existing asset replaced", err)
	}
}
func TestAssetsConcurrentFreezeAndInterruptedTransfers(t *testing.T) {
	a, err := NewAssets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte{42}, 1024)
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			if _, err := a.Save(t.Context(), bytes.NewReader(data), "same.bin", "application/octet-stream", 1024); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	rows, err := os.ReadDir(a.root)
	if err != nil || len(rows) != 1 {
		t.Fatal("asset deduplication failed", len(rows), err)
	}
	_, err = a.Save(t.Context(), bytes.NewReader(data), "large.bin", "application/octet-stream", 1023)
	var refusal *providers.Error
	if !errors.As(err, &refusal) || refusal.Status != 413 {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := a.Save(ctx, bytes.NewReader(data), "cancelled.bin", "application/octet-stream", 1024); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	rows, err = os.ReadDir(a.root)
	if err != nil || len(rows) != 1 {
		t.Fatal("partial transfer retained", len(rows), err)
	}
}
func TestAssetsRejectSymlinkedOwnedDirectory(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "bifrost-go-assets")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAssets(root); !errors.Is(err, ErrAsset) {
		t.Fatal(err)
	}
}
