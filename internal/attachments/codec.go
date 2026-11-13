package attachments

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/veryshyjelly/bifrost/internal/providers"
)

//go:embed webp_helper.py
var webpHelper []byte

//go:embed lottie_light.min.js
var lottiePlayer string

const WhatsAppStickerBudget int64 = 1_000_000

type CodecOptions struct {
	Python, FFmpeg, FFprobe, Chromium string
	NoSandbox                         bool
	Timeout                           time.Duration
}
type Codec struct {
	assets *Assets
	c      CodecOptions
}
type Frames struct {
	Width          int       `json:"width"`
	Height         int       `json:"height"`
	Count          int       `json:"count"`
	Duration       float64   `json:"duration"`
	Durations      []float64 `json:"durations"`
	NormalizeAlpha bool      `json:"normalizeAlpha"`
}
type probeStream struct {
	CodecType     string `json:"codec_type"`
	CodecName     string `json:"codec_name"`
	Width, Height int
	FrameRate     string `json:"avg_frame_rate"`
}
type probeResult struct {
	Streams []probeStream
	Format  struct{ Duration string }
}

func Unsupported() error {
	return &providers.Error{Service: "media", Status: 415, Code: "NATIVE_CODEC_UNAVAILABLE"}
}
func NewCodec(assets *Assets, c CodecOptions) (*Codec, error) {
	if assets == nil {
		return nil, ErrAsset
	}
	if c.Python == "" {
		c.Python = "python3"
	}
	if c.FFmpeg == "" {
		c.FFmpeg = "ffmpeg"
	}
	if c.FFprobe == "" {
		c.FFprobe = "ffprobe"
	}
	if c.Timeout == 0 {
		c.Timeout = 2 * time.Minute
	}
	if c.Timeout < time.Second || c.Timeout > 10*time.Minute {
		return nil, Unsupported()
	}
	return &Codec{assets: assets, c: c}, nil
}

type boundedOutput struct {
	buffer    bytes.Buffer
	remaining int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > b.remaining {
		return 0, Unsupported()
	}
	b.remaining -= len(p)
	return b.buffer.Write(p)
}
func (c *Codec) run(ctx context.Context, dir, command string, args ...string) ([]byte, error) {
	return c.runBounded(ctx, dir, 65536, command, args...)
}
func (c *Codec) runBounded(ctx context.Context, dir string, limit int, command string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "PYTHONNOUSERSITE=1"}
	out := &boundedOutput{remaining: limit}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, Unsupported()
	}
	return out.buffer.Bytes(), nil
}
func (c *Codec) inspect(ctx context.Context, dir, helper, input string, decode bool) (Frames, error) {
	args := []string{helper, "inspect", input}
	if decode {
		args = append(args, dir)
	}
	raw, err := c.run(ctx, dir, c.c.Python, args...)
	if err != nil {
		return Frames{}, err
	}
	var f Frames
	if json.Unmarshal(raw, &f) != nil || f.Width <= 0 || f.Height <= 0 || f.Width > 2048 || f.Height > 2048 || f.Count <= 0 || f.Count > 1200 || len(f.Durations) != f.Count || !finite(f.Duration) || f.Duration <= 0 || f.Duration > 60 {
		return f, Unsupported()
	}
	if int64(f.Width)*int64(f.Height)*int64(f.Count) > 512*512*1200 {
		return f, Unsupported()
	}
	for _, d := range f.Durations {
		if !finite(d) || d <= 0 {
			return f, Unsupported()
		}
	}
	return f, nil
}
func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }
func validTiming(f Frames, max float64) bool {
	if f.Duration > max+.001 {
		return false
	}
	if f.Count > 1 {
		for _, d := range f.Durations {
			if d < 8 {
				return false
			}
		}
	}
	return true
}
func (c *Codec) probe(ctx context.Context, dir, input string) (probeResult, error) {
	raw, err := c.run(ctx, dir, c.c.FFprobe, "-v", "error", "-show_format", "-show_streams", "-of", "json", input)
	if err != nil {
		return probeResult{}, err
	}
	var info probeResult
	if json.Unmarshal(raw, &info) != nil {
		return info, Unsupported()
	}
	return info, nil
}
func rate(s string) float64 {
	p := strings.Split(s, "/")
	if len(p) != 2 {
		return 0
	}
	n, e := strconv.ParseFloat(p[0], 64)
	if e != nil {
		return 0
	}
	d, e := strconv.ParseFloat(p[1], 64)
	if e != nil || d == 0 {
		return 0
	}
	return n / d
}
func validVideo(info probeResult) bool {
	if len(info.Streams) != 1 {
		return false
	}
	s := info.Streams[0]
	duration, err := strconv.ParseFloat(info.Format.Duration, 64)
	return err == nil && finite(duration) && duration > 0 && duration <= 3.001 && s.CodecType == "video" && s.CodecName == "vp9" && max(s.Width, s.Height) == 512 && min(s.Width, s.Height) > 0 && finite(rate(s.FrameRate)) && rate(s.FrameRate) > 0 && rate(s.FrameRate) <= 30
}
func fileFits(path string, max int64) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() <= max
}
func (c *Codec) saveOutput(ctx context.Context, path, name, mime, format string, max int64) (Asset, error) {
	file, err := os.Open(path)
	if err != nil {
		return Asset{}, Unsupported()
	}
	defer file.Close()
	asset, err := c.assets.Save(ctx, file, name, mime, max)
	asset.StickerFormat = format
	return asset, err
}
func (c *Codec) Transcode(ctx context.Context, input Asset, voice bool, maxBytes int64) (Asset, error) {
	ctx, cancel := context.WithTimeout(ctx, c.c.Timeout)
	defer cancel()
	file, err := c.assets.Open(ctx, input)
	if err != nil {
		return Asset{}, err
	}
	file.Close()
	path, _ := c.assets.Path(input)
	dir, err := os.MkdirTemp(c.assets.root, ".codec-")
	if err != nil {
		return Asset{}, ErrAsset
	}
	defer os.RemoveAll(dir)
	name, mime := "image.jpg", "image/jpeg"
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", path}
	if voice {
		name, mime = "voice.ogg", "audio/ogg"
		args = append(args, "-vn", "-c:a", "libopus", "-b:a", "48k")
	} else {
		args = append(args, "-frames:v", "1")
	}
	output := filepath.Join(dir, name)
	args = append(args, output)
	if _, err := c.run(ctx, dir, c.c.FFmpeg, args...); err != nil {
		return Asset{}, err
	}
	return c.saveOutput(ctx, output, name, mime, "", maxBytes)
}
func (c *Codec) Sticker(ctx context.Context, input Asset, target string) (Asset, error) {
	if target != "whatsapp" && target != "telegram" {
		return Asset{}, Unsupported()
	}
	ctx, cancel := context.WithTimeout(ctx, c.c.Timeout)
	defer cancel()
	file, err := c.assets.Open(ctx, input)
	if err != nil {
		return Asset{}, err
	}
	header := make([]byte, 12)
	n, _ := io.ReadFull(file, header)
	file.Close()
	path, _ := c.assets.Path(input)
	dir, err := os.MkdirTemp(c.assets.root, ".codec-")
	if err != nil {
		return Asset{}, ErrAsset
	}
	defer os.RemoveAll(dir)
	helper := filepath.Join(dir, "webp_helper.py")
	if err := os.WriteFile(helper, webpHelper, 0600); err != nil {
		return Asset{}, ErrAsset
	}
	var frames Frames
	switch {
	case n >= 12 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WEBP":
		frames, err = c.inspect(ctx, dir, helper, path, false)
		if err != nil {
			return Asset{}, err
		}
		limit := WhatsAppStickerBudget
		if target == "telegram" {
			limit = 100000
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return Asset{}, ErrAsset
		}
		_, structureErr := InspectWebP(raw)
		if structureErr == nil && frames.Width == 512 && frames.Height == 512 && (target == "whatsapp" || frames.Count == 1) && !frames.NormalizeAlpha && validTiming(frames, 10) && input.Size <= limit {
			input.MIME = "image/webp"
			input.Name = "sticker.webp"
			input.StickerFormat = "static"
			if frames.Count > 1 {
				input.StickerFormat = "animated"
			}
			return input, nil
		}
		frames, err = c.inspect(ctx, dir, helper, path, true)
	case n >= 2 && header[0] == 0x1f && header[1] == 0x8b:
		frames, err = c.renderTGS(ctx, dir, path)
	case n >= 4 && bytes.Equal(header[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}):
		info, probeErr := c.probe(ctx, dir, path)
		if probeErr != nil {
			return Asset{}, probeErr
		}
		if target == "telegram" && input.Size <= 256000 && validVideo(info) {
			input.MIME = "video/webm"
			input.Name = "sticker.webm"
			input.StickerFormat = "video"
			return input, nil
		}
		frames, err = c.decodeVideo(ctx, dir, path, info)
	default:
		return Asset{}, Unsupported()
	}
	if err != nil {
		return Asset{}, err
	}
	raw, _ := json.Marshal(frames)
	if err := os.WriteFile(filepath.Join(dir, "frames.json"), raw, 0600); err != nil {
		return Asset{}, ErrAsset
	}
	if target == "whatsapp" || frames.Count == 1 {
		return c.encodeWebP(ctx, dir, helper, frames, target)
	}
	return c.encodeTelegramVideo(ctx, dir, frames)
}
func (c *Codec) encodeWebP(ctx context.Context, dir, helper string, frames Frames, target string) (Asset, error) {
	output := filepath.Join(dir, "output.webp")
	limit := WhatsAppStickerBudget
	if target == "telegram" {
		limit = 100000
	}
	for _, attempt := range [][2]int{{80, 30}, {60, 30}, {40, 30}, {40, 20}, {40, 15}, {30, 15}, {30, 12}, {30, 10}} {
		if _, err := c.run(ctx, dir, c.c.Python, helper, "encode", dir, output, strconv.Itoa(attempt[0]), strconv.Itoa(attempt[1])); err != nil {
			return Asset{}, err
		}
		if !fileFits(output, limit) {
			continue
		}
		result, err := c.inspect(ctx, dir, helper, output, false)
		if err != nil {
			return Asset{}, err
		}
		raw, err := os.ReadFile(output)
		if err != nil {
			return Asset{}, ErrAsset
		}
		if _, err := InspectWebP(raw); err != nil {
			return Asset{}, Unsupported()
		}
		if result.Width != 512 || result.Height != 512 || !validTiming(result, 10) || (frames.Count > 1 && result.Count < 2) {
			return Asset{}, Unsupported()
		}
		format := "static"
		if result.Count > 1 {
			format = "animated"
		}
		return c.saveOutput(ctx, output, "sticker.webp", "image/webp", format, limit)
	}
	return Asset{}, Unsupported()
}
func (c *Codec) decodeVideo(ctx context.Context, dir, input string, info probeResult) (Frames, error) {
	var stream *probeStream
	for i := range info.Streams {
		if info.Streams[i].CodecType == "video" {
			stream = &info.Streams[i]
			break
		}
	}
	if stream == nil || stream.Width <= 0 || stream.Height <= 0 || stream.Width > 2048 || stream.Height > 2048 || (stream.CodecName != "vp8" && stream.CodecName != "vp9") {
		return Frames{}, Unsupported()
	}
	// Some Telegram WebMs have a millisecond-scale Segment Duration even though
	// their packets span several seconds. Trust their playback timestamps instead
	// of truncating a moving sticker to the container's claimed duration.
	raw, err := c.runBounded(ctx, dir, 512<<10, c.c.FFprobe, "-v", "error", "-select_streams", "v:0", "-show_packets", "-show_entries", "packet=pts_time,duration_time", "-of", "compact=p=0", input)
	if err != nil {
		return Frames{}, err
	}
	duration, err := videoPacketDuration(raw, stream.FrameRate)
	if err != nil {
		return Frames{}, err
	}
	outputDuration := min(duration, 10)
	decoder := "libvpx"
	if stream.CodecName == "vp9" {
		decoder = "libvpx-vp9"
	}
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-c:v", decoder, "-i", input, "-an", "-vf", fmt.Sprintf("setpts=%g*(PTS-STARTPTS),fps=30,scale=512:512:force_original_aspect_ratio=decrease,format=rgba,pad=512:512:(ow-iw)/2:(oh-ih)/2:color=0x00000000", outputDuration/duration), "-t", strconv.FormatFloat(outputDuration, 'f', 6, 64), "-frames:v", "300", "-start_number", "0", filepath.Join(dir, "frame%04d.png")}
	if _, err := c.run(ctx, dir, c.c.FFmpeg, args...); err != nil {
		return Frames{}, err
	}
	files, err := filepath.Glob(filepath.Join(dir, "frame*.png"))
	if err != nil || len(files) == 0 || len(files) > 300 {
		return Frames{}, Unsupported()
	}
	frames := Frames{Width: 512, Height: 512, Count: len(files), Duration: float64(len(files)) / 30}
	for range files {
		frames.Durations = append(frames.Durations, 1000.0/30)
	}
	return frames, nil
}
func (c *Codec) encodeTelegramVideo(ctx context.Context, dir string, frames Frames) (Asset, error) {
	var lines strings.Builder
	for i, duration := range frames.Durations {
		fmt.Fprintf(&lines, "file 'frame%04d.png'\nduration %g\n", i, duration/1000)
	}
	fmt.Fprintf(&lines, "file 'frame%04d.png'\n", frames.Count-1)
	list := filepath.Join(dir, "frames.txt")
	if err := os.WriteFile(list, []byte(lines.String()), 0600); err != nil {
		return Asset{}, ErrAsset
	}
	output := filepath.Join(dir, "output.webm")
	duration := min(frames.Duration, 3)
	for _, attempt := range [][2]int{{32, 30}, {42, 30}, {52, 30}, {56, 20}, {60, 15}} {
		args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-f", "concat", "-safe", "0", "-i", list, "-an", "-vf", fmt.Sprintf("setpts=%g*(PTS-STARTPTS),fps=%d,scale=512:512:force_original_aspect_ratio=decrease,format=rgba,pad=512:512:(ow-iw)/2:(oh-ih)/2:color=0x00000000", duration/frames.Duration, attempt[1]), "-t", strconv.FormatFloat(duration, 'f', 6, 64), "-c:v", "libvpx-vp9", "-pix_fmt", "yuva420p", "-b:v", "0", "-crf", strconv.Itoa(attempt[0]), "-auto-alt-ref", "0", "-threads", "2", output}
		if _, err := c.run(ctx, dir, c.c.FFmpeg, args...); err != nil {
			return Asset{}, err
		}
		if !fileFits(output, 256000) {
			continue
		}
		info, err := c.probe(ctx, dir, output)
		if err != nil {
			return Asset{}, err
		}
		if !validVideo(info) {
			return Asset{}, Unsupported()
		}
		return c.saveOutput(ctx, output, "sticker.webm", "video/webm", "video", 256000)
	}
	return Asset{}, Unsupported()
}
