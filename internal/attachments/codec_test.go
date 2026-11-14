package attachments

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func codecFixture(t *testing.T) (*Assets, *Codec, string) {
	t.Helper()
	if os.Getenv("BIFROST_TEST_CODECS") != "1" {
		t.Skip("set BIFROST_TEST_CODECS=1 with pinned Python, ffmpeg and Chromium installed")
	}
	python := os.Getenv("STICKER_PYTHON")
	if python == "" {
		python = "python3"
	}
	a, err := NewAssets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCodec(a, CodecOptions{Python: python, Chromium: os.Getenv("STICKER_CHROMIUM"), NoSandbox: os.Getenv("STICKER_CHROMIUM_NO_SANDBOX") == "1", Timeout: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return a, c, python
}

func assertStickerCacheReuse(t *testing.T, assets *Assets, source, result Asset, target string) {
	t.Helper()
	restarted, err := NewCodec(assets, CodecOptions{Python: "/missing/python", FFmpeg: "/missing/ffmpeg", FFprobe: "/missing/ffprobe", Chromium: "/missing/chromium"})
	if err != nil {
		t.Fatal(err)
	}
	if cached, err := restarted.Sticker(t.Context(), source, target); err != nil || cached != result {
		t.Fatal("cached format/motion changed or conversion ran again", cached, err)
	}
}
func pythonFixture(t *testing.T, python, script string, args ...string) {
	t.Helper()
	command := exec.Command(python, append([]string{"-c", script}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("codec fixture failed: %v\n%s", err, output)
	}
}
func loadCodecAsset(t *testing.T, a *Assets, path, name, mime string) Asset {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	asset, err := a.Save(t.Context(), file, name, mime, 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	return asset
}
func TestCodecPreservesDenseStaticAndAnimatedWebPWithinApprovedBudget(t *testing.T) {
	a, c, python := codecFixture(t)
	for _, animated := range []bool{false, true} {
		t.Run(map[bool]string{false: "static", true: "animated"}[animated], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input.webp")
			mode := "static"
			if animated {
				mode = "animated"
			}
			pythonFixture(t, python, `from PIL import Image
import random,sys
r=random.Random(7)
animated=sys.argv[2]=='animated'
def frame():
    data=bytes(r.randrange(8)*32 if animated else r.randrange(256) for _ in range(512*512*3))
    return Image.frombytes('RGB',(512,512),data).convert('RGBA')
a=frame()
a.save(sys.argv[1],format='WEBP',lossless=True,save_all=animated,append_images=[frame()] if animated else [],duration=[200,200],loop=0,method=4)
data=bytearray(open(sys.argv[1],'rb').read())
if animated: data[20]|=16
open(sys.argv[1],'wb').write(data)
`, path, mode)
			source := loadCodecAsset(t, a, path, "original.webp", "image/webp")
			threshold := int64(100000)
			if animated {
				threshold = 500000
			}
			if source.Size <= threshold || source.Size > WhatsAppStickerBudget {
				t.Fatal("fixture does not exercise larger budget", source.Size)
			}
			result, err := c.Sticker(t.Context(), source, "whatsapp")
			if err != nil {
				t.Fatal(err)
			}
			if result.Hash != source.Hash || result.Size != source.Size {
				t.Fatal("valid sticker re-encoded", source.Size, result.Size)
			}
			expected := "static"
			if animated {
				expected = "animated"
			}
			if result.StickerFormat != expected {
				t.Fatal(result.StickerFormat)
			}
			raw, _ := os.ReadFile(path)
			if _, err := InspectWebP(raw); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestCodecTelegramAnimationMeetsVideoLimitsAndRemainsReusable(t *testing.T) {
	a, c, python := codecFixture(t)
	path := filepath.Join(t.TempDir(), "input.webp")
	pythonFixture(t, python, `from PIL import Image
import sys
a=Image.new('RGBA',(512,512),(255,0,0,128));b=Image.new('RGBA',(512,512),(0,0,255,128))
a.save(sys.argv[1],format='WEBP',save_all=True,append_images=[b],duration=[100,100],loop=0)
`, path)
	source := loadCodecAsset(t, a, path, "original.webp", "image/webp")
	result, err := c.Sticker(t.Context(), source, "telegram")
	if err != nil {
		t.Fatal(err)
	}
	if result.StickerFormat != "video" || result.Size > 256000 {
		t.Fatal(result.StickerFormat, result.Size)
	}
	assertStickerCacheReuse(t, a, source, result, "telegram")
	again, err := c.Sticker(t.Context(), result, "telegram")
	if err != nil || again.Hash != result.Hash {
		t.Fatal("valid archived video changed", err)
	}
}

func TestCodecVideoUsesPacketTimingWhenContainerDurationIsWrong(t *testing.T) {
	a, c, python := codecFixture(t)
	path := filepath.Join(t.TempDir(), "input.webm")
	command := exec.Command(c.c.FFmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=128x128:rate=24:duration=2", "-c:v", "libvpx-vp9", "-threads", "2", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("video fixture: %v\n%s", err, output)
	}
	// Reproduce the retained Telegram source: a tiny EBML Segment Duration,
	// while the original packets retain their complete two-second timeline.
	pythonFixture(t, python, `import struct,sys
from pathlib import Path
p=Path(sys.argv[1]);b=bytearray(p.read_bytes());marker=b'\x44\x89\x88'
assert b.count(marker)==1
offset=b.index(marker)+len(marker)
b[offset:offset+8]=struct.pack('>d',2.003)
p.write_bytes(b)
`, path)
	info, err := c.probe(t.Context(), filepath.Dir(path), path)
	if err != nil || info.Format.Duration != "0.002003" {
		t.Fatal("fixture no longer reproduces malformed duration", info.Format.Duration, err)
	}
	source := loadCodecAsset(t, a, path, "original.webm", "video/webm")
	result, err := c.Sticker(t.Context(), source, "whatsapp")
	if err != nil || result.StickerFormat != "animated" || result.Size > WhatsAppStickerBudget {
		t.Fatal("animation conversion", result, err)
	}
	assertStickerCacheReuse(t, a, source, result, "whatsapp")
	output, _ := a.Path(result)
	pythonFixture(t, python, `from PIL import Image
import hashlib,sys
im=Image.open(sys.argv[1]);assert im.n_frames>=30
hashes=set();duration=0
for i in range(im.n_frames):
    im.seek(i);hashes.add(hashlib.sha256(im.convert('RGBA').tobytes()).digest());duration+=im.info['duration']
assert len(hashes)>20 and 1900<=duration<=2100,(len(hashes),duration)
`, output)
}

const tgsFixture = `{"v":"5.7.4","fr":20,"ip":10,"op":70,"w":512,"h":512,"nm":"fixture","assets":[],"layers":[{"ty":4,"ind":1,"ip":10,"op":70,"st":0,"ks":{"o":{"a":0,"k":100},"r":{"a":0,"k":0},"p":{"a":1,"k":[{"t":10,"s":[75,125,0],"e":[375,125,0],"o":{"x":0.33,"y":0.33},"i":{"x":0.67,"y":0.67}},{"t":70,"s":[375,125,0]}]},"a":{"a":0,"k":[0,0,0]},"s":{"a":0,"k":[100,100,100]}},"shapes":[{"ty":"rc","p":{"a":0,"k":[0,0]},"s":{"a":0,"k":[50,50]},"r":{"a":0,"k":0}},{"ty":"fl","c":{"a":0,"k":[1,0,0,1]},"o":{"a":0,"k":100},"r":1}]}]}`

func gzipFixture(t *testing.T, data string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestCodecTGSUsesValidatedLottieMotionAndTransparentCanvas(t *testing.T) {
	a, c, python := codecFixture(t)
	asset, err := a.Save(t.Context(), bytes.NewReader(gzipFixture(t, tgsFixture)), "fixture.tgs", "application/x-tgsticker", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.Sticker(t.Context(), asset, "whatsapp")
	if err != nil {
		t.Fatalf("TGS conversion failed: %#v", err)
	}
	if result.StickerFormat != "animated" || result.Size > WhatsAppStickerBudget {
		t.Fatal(result.StickerFormat, result.Size)
	}
	assertStickerCacheReuse(t, a, asset, result, "whatsapp")
	path, _ := a.Path(result)
	script := `from PIL import Image
import json,sys
im=Image.open(sys.argv[1]);first=im.convert('RGBA').getpixel((75,125));duration=0
for i in range(im.n_frames):
    im.seek(i);im.load();duration+=im.info['duration']
last=im.convert('RGBA');print(json.dumps([duration,first[0],last.getpixel((370,125))[0],last.getpixel((0,0))[3]]))`
	output, err := exec.Command(python, "-c", script, path).Output()
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	if json.Unmarshal(output, &got) != nil || len(got) != 4 || got[0] < 2960 || got[0] > 3040 || got[1] < 200 || got[2] < 200 || got[3] != 0 {
		t.Fatal("motion or alpha lost", string(output))
	}
}
func TestTGSRejectsExpressionsExternalAssetsAndMalformedDataBeforeRendering(t *testing.T) {
	for _, data := range []string{`invalid`, `{"w":512,"h":512,"fr":30,"ip":0,"op":30,"x":"danger"}`, `{"w":512,"h":512,"fr":30,"ip":0,"op":30,"assets":[{"p":"https://example.invalid/image.png"}]}`, `{"w":10000,"h":512,"fr":30,"ip":0,"op":30}`} {
		path := filepath.Join(t.TempDir(), "invalid.tgs")
		if err := os.WriteFile(path, gzipFixture(t, data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := readTGS(path); err == nil {
			t.Fatal("invalid Lottie accepted")
		}
	}
}
