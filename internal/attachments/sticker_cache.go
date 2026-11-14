package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Bump when Go conversion/timing/validation policy changes. Embedded renderer
// changes and the outgoing WebP budget also invalidate the persistent lookup.
const stickerCacheRevision = "1-packet-timing"

type stickerCacheEntry struct {
	Revision  string
	InputHash string
	InputSize int64
	Target    string
	Output    Asset
}

type stickerFlight struct {
	done   chan struct{}
	output Asset
	err    error
}

func (c *Codec) Sticker(ctx context.Context, input Asset, target string) (Asset, error) {
	if target != "whatsapp" && target != "telegram" {
		return Asset{}, Unsupported()
	}
	if ctx == nil {
		return Asset{}, ErrAsset
	}
	ctx, cancel := context.WithTimeout(ctx, c.c.Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	// A cached result never substitutes for verifying the retained source bytes.
	file, err := c.assets.Open(ctx, input)
	if err != nil {
		return Asset{}, err
	}
	file.Close()
	return c.cachedSticker(ctx, input, target, func(ctx context.Context) (Asset, error) {
		return c.convertSticker(ctx, input, target)
	})
}

func stickerCacheKey(input Asset, target string) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\x00%s\x00%d\x00%s\x00%d\x00", stickerCacheRevision, input.Hash, input.Size, target, WhatsAppStickerBudget)
	hash.Write(webpHelper)
	hash.Write([]byte(lottiePlayer))
	return hex.EncodeToString(hash.Sum(nil))
}

func (c *Codec) cachedSticker(ctx context.Context, input Asset, target string, convert func(context.Context) (Asset, error)) (output Asset, err error) {
	key := stickerCacheKey(input, target)
	c.mu.Lock()
	if flight := c.stickerFlights[key]; flight != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return Asset{}, ctx.Err()
		case <-flight.done:
			return flight.output, flight.err
		}
	}
	flight := &stickerFlight{done: make(chan struct{})}
	if c.stickerFlights == nil {
		c.stickerFlights = make(map[string]*stickerFlight)
	}
	c.stickerFlights[key] = flight
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		flight.output, flight.err = output, err
		delete(c.stickerFlights, key)
		close(flight.done)
		c.mu.Unlock()
	}()
	if output, ok := c.readStickerCache(ctx, key, input, target); ok {
		return output, nil
	}
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	output, err = convert(ctx)
	if err == nil && ctx.Err() == nil {
		// Cache storage is an optimization: its failure cannot discard a valid
		// conversion or change the original-document fallback/delivery journal.
		c.writeStickerCache(key, stickerCacheEntry{stickerCacheRevision, input.Hash, input.Size, target, output})
	}
	return output, err
}

func (c *Codec) readStickerCache(ctx context.Context, key string, input Asset, target string) (Asset, bool) {
	path := filepath.Join(c.assets.root, ".sticker-cache-"+key+".json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return Asset{}, false
	}
	file, err := os.Open(path)
	if err != nil {
		return Asset{}, false
	}
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	file.Close()
	var entry stickerCacheEntry
	if err != nil || len(raw) > 4096 || json.Unmarshal(raw, &entry) != nil || entry.Revision != stickerCacheRevision || entry.InputHash != input.Hash || entry.InputSize != input.Size || entry.Target != target {
		return Asset{}, false
	}
	output := entry.Output
	limit := WhatsAppStickerBudget
	if target == "telegram" {
		limit = 100000
		if output.StickerFormat == "video" {
			limit = 256000
		}
	}
	if output.Size <= 0 || output.Size > limit {
		return Asset{}, false
	}
	file, err = c.assets.Open(ctx, output)
	if err != nil {
		return Asset{}, false
	}
	raw, err = io.ReadAll(io.LimitReader(file, limit+1))
	file.Close()
	if err != nil || int64(len(raw)) != output.Size {
		return Asset{}, false
	}
	if output.MIME == "video/webm" && output.Name == "sticker.webm" && output.StickerFormat == "video" && target == "telegram" && bytes.HasPrefix(raw, []byte{0x1a, 0x45, 0xdf, 0xa3}) {
		return output, true // Exact hash of a previously probed/validated video.
	}
	if output.MIME != "image/webp" || output.Name != "sticker.webp" {
		return Asset{}, false
	}
	webp, err := InspectWebP(raw)
	if err != nil || webp.Width != 512 || webp.Height != 512 {
		return Asset{}, false
	}
	if output.StickerFormat == "static" && webp.Frames == 1 && !webp.Animated {
		return output, true
	}
	if output.StickerFormat != "animated" || target != "whatsapp" || !webp.Animated || webp.Frames < 2 {
		return Asset{}, false
	}
	duration := 0
	for _, ms := range webp.Durations {
		if ms < 8 {
			return Asset{}, false
		}
		duration += ms
	}
	return output, duration > 0 && duration <= 10000
}

func (c *Codec) writeStickerCache(key string, entry stickerCacheEntry) {
	file, err := os.CreateTemp(c.assets.root, ".incoming-sticker-cache-")
	if err != nil {
		return
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if json.NewEncoder(file).Encode(entry) != nil || file.Sync() != nil || file.Close() != nil {
		return
	}
	if os.Rename(file.Name(), filepath.Join(c.assets.root, ".sticker-cache-"+key+".json")) != nil {
		return
	}
	if directory, err := os.Open(c.assets.root); err == nil {
		directory.Sync()
		directory.Close()
	}
}
