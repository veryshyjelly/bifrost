package attachments

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func stickerCacheFixture(t *testing.T) (*Assets, *Codec, Asset, Asset) {
	t.Helper()
	a, err := NewAssets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCodec(a, CodecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	input, err := a.Save(t.Context(), bytes.NewBufferString("retained source sticker"), "source.tgs", "application/x-tgsticker", 1024)
	if err != nil {
		t.Fatal(err)
	}
	// A complete 512x512 lossless WebP, produced by Pillow; no external tools
	// are needed for the persistence/concurrency/integrity tests.
	raw, err := base64.StdEncoding.DecodeString("UklGRiwAAABXRUJQVlA4TCAAAAAv/8F/AAcQ/Y/+BwQCyf7eMxTR/4z//Oc///nPf/7zfw==")
	if err != nil {
		t.Fatal(err)
	}
	output, err := a.Save(t.Context(), bytes.NewReader(raw), "sticker.webp", "image/webp", WhatsAppStickerBudget)
	if err != nil {
		t.Fatal(err)
	}
	output.StickerFormat = "static"
	return a, c, input, output
}

func TestStickerCachePersistsAndSeparatesSourceTargetAndRevision(t *testing.T) {
	a, c, input, output := stickerCacheFixture(t)
	calls := 0
	convert := func(context.Context) (Asset, error) { calls++; return output, nil }
	if got, err := c.cachedSticker(t.Context(), input, "whatsapp", convert); err != nil || got != output {
		t.Fatal(got, err)
	}
	// A new codec has no in-memory conversion state. Tools are deliberately
	// unavailable: a hit must not probe, decode or render the sticker again.
	restarted, err := NewCodec(a, CodecOptions{Python: "/missing/python", FFmpeg: "/missing/ffmpeg", FFprobe: "/missing/ffprobe", Chromium: "/missing/chromium"})
	if err != nil {
		t.Fatal(err)
	}
	renamed := input
	renamed.Name, renamed.MIME = "different-name.bin", "application/octet-stream"
	if got, err := restarted.Sticker(t.Context(), renamed, "whatsapp"); err != nil || got != output {
		t.Fatal("persistent hit", got, err)
	}
	if _, err := restarted.cachedSticker(t.Context(), input, "telegram", convert); err != nil {
		t.Fatal(err)
	}
	other, err := a.Save(t.Context(), bytes.NewBufferString("different retained sticker"), "source.tgs", input.MIME, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.cachedSticker(t.Context(), other, "whatsapp", convert); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.root, ".sticker-cache-"+stickerCacheKey(input, "whatsapp")+".json")
	entry := stickerCacheEntry{"obsolete-codec", input.Hash, input.Size, "whatsapp", output}
	raw, _ := json.Marshal(entry)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.cachedSticker(t.Context(), input, "whatsapp", convert); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatal("conversion count", calls)
	}
	// Abandoned-work cleanup must leave persistent conversion lookups intact.
	if err := a.Cleanup(t.Context(), time.Now().Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, err := restarted.Sticker(t.Context(), input, "whatsapp"); err != nil || got != output {
		t.Fatal("cleanup removed cache", got, err)
	}
}

func TestStickerCacheIgnoresInvalidEntriesAndMissingBytes(t *testing.T) {
	for _, name := range []string{"json", "target", "metadata", "size", "missing-output", "symlink"} {
		t.Run(name, func(t *testing.T) {
			a, c, input, output := stickerCacheFixture(t)
			calls := 0
			convert := func(context.Context) (Asset, error) { calls++; return output, nil }
			if _, err := c.cachedSticker(t.Context(), input, "whatsapp", convert); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(a.root, ".sticker-cache-"+stickerCacheKey(input, "whatsapp")+".json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var entry stickerCacheEntry
			if err := json.Unmarshal(raw, &entry); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "json":
				raw = []byte("partial cache write")
			case "target":
				entry.Target = "telegram"
				raw, _ = json.Marshal(entry)
			case "metadata":
				entry.Output.StickerFormat = "animated"
				raw, _ = json.Marshal(entry)
			case "size":
				entry.Output.Size = WhatsAppStickerBudget + 1
				raw, _ = json.Marshal(entry)
			case "missing-output":
				blob, _ := a.Path(output)
				if err := os.Remove(blob); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside.json")
				if err := os.WriteFile(outside, raw, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			}
			if name != "symlink" {
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := c.cachedSticker(t.Context(), input, "whatsapp", convert); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatal("invalid cache reused", calls)
			}
		})
	}
}

func TestStickerCacheRefusesDamagedSourceAndOutput(t *testing.T) {
	a, c, input, output := stickerCacheFixture(t)
	if _, err := c.cachedSticker(t.Context(), input, "whatsapp", func(context.Context) (Asset, error) { return output, nil }); err != nil {
		t.Fatal(err)
	}
	path, _ := a.Path(output)
	raw := bytes.Repeat([]byte("x"), int(output.Size))
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	if _, err := c.cachedSticker(t.Context(), input, "whatsapp", func(context.Context) (Asset, error) { called = true; return Asset{}, Unsupported() }); err == nil || !called {
		t.Fatal("damaged output reused", err)
	}
	path, _ = a.Path(input)
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), int(input.Size)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Sticker(t.Context(), input, "whatsapp"); !errors.Is(err, ErrAsset) {
		t.Fatal("damaged source bypassed verification", err)
	}
}

func TestStickerCacheCoalescesAndCancelsWaiterWithoutPoisoningResult(t *testing.T) {
	_, c, input, output := stickerCacheFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	var calls atomic.Int32
	convert := func(context.Context) (Asset, error) { calls.Add(1); close(started); <-release; return output, nil }
	go func() { _, err := c.cachedSticker(t.Context(), input, "whatsapp", convert); result <- err }()
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := c.cachedSticker(ctx, input, "whatsapp", func(context.Context) (Asset, error) { t.Error("duplicate conversion"); return Asset{}, Unsupported() })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if got, err := c.Sticker(t.Context(), input, "whatsapp"); err != nil || got != output || calls.Load() != 1 {
		t.Fatal(got, err, calls.Load())
	}
}

func TestStickerCacheDoesNotCacheFailuresOrCancelledConversion(t *testing.T) {
	for _, cancelConversion := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "cancelled"}[cancelConversion], func(t *testing.T) {
			_, c, input, output := stickerCacheFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			_, _ = c.cachedSticker(ctx, input, "whatsapp", func(context.Context) (Asset, error) {
				if cancelConversion {
					cancel()
					return output, nil
				}
				return Asset{}, Unsupported()
			})
			called := false
			got, err := c.cachedSticker(t.Context(), input, "whatsapp", func(context.Context) (Asset, error) { called = true; return output, nil })
			if err != nil || got != output || !called {
				t.Fatal(got, err, called)
			}
		})
	}
}

func TestStickerCacheWriteFailurePreservesSuccessfulConversion(t *testing.T) {
	a, c, input, output := stickerCacheFixture(t)
	path := filepath.Join(a.root, ".sticker-cache-"+stickerCacheKey(input, "whatsapp")+".json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	calls := 0
	for range 2 {
		got, err := c.cachedSticker(t.Context(), input, "whatsapp", func(context.Context) (Asset, error) {
			calls++
			return output, nil
		})
		if err != nil || got != output {
			t.Fatal("cache failure discarded valid conversion", got, err)
		}
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
