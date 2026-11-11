package attachments

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func readTGS(input string) ([]byte, float64, float64, error) {
	file, err := os.Open(input)
	if err != nil {
		return nil, 0, 0, ErrAsset
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, 0, 0, Unsupported()
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return nil, 0, 0, Unsupported()
	}
	var data map[string]any
	if json.Unmarshal(raw, &data) != nil {
		return nil, 0, 0, Unsupported()
	}
	number := func(key string) float64 {
		n, ok := data[key].(float64)
		if !ok {
			return math.NaN()
		}
		return n
	}
	width, height, ip, op, fr := number("w"), number("h"), number("ip"), number("op"), number("fr")
	duration := (op - ip) / fr
	if !finite(width) || !finite(height) || !finite(ip) || !finite(op) || !finite(fr) || !finite(duration) || width <= 0 || height <= 0 || max(width, height) > 2048 || fr <= 0 || duration <= 0 || duration > 60 {
		return nil, 0, 0, Unsupported()
	}
	pending := []any{data}
	nodes := 0
	for len(pending) > 0 {
		n := len(pending) - 1
		value := pending[n]
		pending = pending[:n]
		nodes++
		if nodes > 500000 {
			return nil, 0, 0, Unsupported()
		}
		switch v := value.(type) {
		case map[string]any:
			if _, ok := v["x"].(string); ok {
				return nil, 0, 0, Unsupported()
			}
			if p, ok := v["p"].(string); ok && !strings.HasPrefix(p, "data:") {
				return nil, 0, 0, Unsupported()
			}
			for _, nested := range v {
				pending = append(pending, nested)
			}
		case []any:
			pending = append(pending, v...)
		}
	}
	return raw, duration, op - ip, nil
}

// Emit alpha=0 explicitly: the generated CDP RGBA uses omitzero for alpha,
// which would otherwise select Chrome's opaque default.
type transparentBackground struct {
	Color struct {
		R int     `json:"r"`
		G int     `json:"g"`
		B int     `json:"b"`
		A float64 `json:"a"`
	} `json:"color"`
}

func (c *Codec) renderTGS(ctx context.Context, dir, input string) (Frames, error) {
	raw, duration, totalFrames, err := readTGS(input)
	if err != nil {
		return Frames{}, err
	}
	outputDuration := min(duration, 10)
	count := max(2, int(math.Ceil(outputDuration*30)))
	options := append([]chromedp.ExecAllocatorOption(nil), chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options, chromedp.NoInheritEnv, chromedp.Env("PATH="+os.Getenv("PATH"), "LANG=C.UTF-8"), chromedp.Flag("disable-dev-shm-usage", true), chromedp.Flag("disable-background-networking", true))
	if c.c.Chromium != "" {
		options = append(options, chromedp.ExecPath(c.c.Chromium))
	}
	if c.c.NoSandbox {
		options = append(options, chromedp.NoSandbox)
	}
	allocator, cancelAllocator := chromedp.NewExecAllocator(ctx, options...)
	defer cancelAllocator()
	browser, cancelBrowser := chromedp.NewContext(allocator, chromedp.WithErrorf(func(string, ...any) {}))
	defer cancelBrowser()
	if err := chromedp.Do(browser, chromedp.Navigate("about:blank")); err != nil {
		return Frames{}, Unsupported()
	}
	if _, err := chromedp.Call(browser, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{Width: 512, Height: 512, DeviceScaleFactor: 1}); err != nil {
		return Frames{}, Unsupported()
	}
	if _, err := chromedp.Call(browser, cdp.Command[transparentBackground, cdp.Empty]{Method: emulation.CommandSetDefaultBackgroundColorOverride}, transparentBackground{}); err != nil {
		return Frames{}, Unsupported()
	}
	if _, err := chromedp.Call(browser, network.Enable, network.EnableParams{}); err != nil {
		return Frames{}, Unsupported()
	}
	patterns := []*network.BlockPattern{}
	for _, scheme := range []string{"http", "https", "ws", "wss", "ftp"} {
		patterns = append(patterns, &network.BlockPattern{URLPattern: scheme + "://*:*/*", Block: true})
	}
	patterns = append(patterns, &network.BlockPattern{URLPattern: "file:///*", Block: true})
	if _, err := chromedp.Call(browser, network.SetBlockedURLs, network.SetBlockedURLsParams{URLPatterns: patterns}); err != nil {
		return Frames{}, Unsupported()
	}
	html := `<html><head><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; font-src data:; style-src 'unsafe-inline'; script-src 'unsafe-inline'"></head><body style="margin:0;background:transparent"><div id="sticker" style="width:512px;height:512px"></div></body></html>`
	document, _ := json.Marshal(html)
	if err := chromedp.Do(browser, chromedp.Evaluate[chromedp.Void]("document.open();document.write("+string(document)+");document.close();"), chromedp.Evaluate[chromedp.Void](lottiePlayer)); err != nil {
		return Frames{}, Unsupported()
	}
	data, _ := json.Marshal(string(raw))
	script := `window.animation=window.lottie.loadAnimation({container:document.getElementById('sticker'),renderer:'svg',loop:false,autoplay:false,animationData:JSON.parse(` + string(data) + `)});window.animation.setSubframe(true);`
	if err := chromedp.Do(browser, chromedp.Evaluate[chromedp.Void](script)); err != nil {
		return Frames{}, Unsupported()
	}
	for {
		ready, err := chromedp.Run(browser, chromedp.Evaluate[bool]("!!(window.animation&&window.animation.isLoaded)"))
		if err != nil {
			return Frames{}, Unsupported()
		}
		if ready {
			break
		}
		select {
		case <-ctx.Done():
			return Frames{}, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	result := Frames{Width: 512, Height: 512, Count: count, Duration: outputDuration}
	for index := 0; index < count; index++ {
		if err := ctx.Err(); err != nil {
			return Frames{}, err
		}
		frame := float64(index) * totalFrames / float64(count)
		if err := chromedp.Do(browser, chromedp.Evaluate[chromedp.Void](fmt.Sprintf("window.animation.goToAndStop(%g,true)", frame))); err != nil {
			return Frames{}, Unsupported()
		}
		screenshot, err := chromedp.Call(browser, page.CaptureScreenshot, page.CaptureScreenshotParams{Format: page.CaptureScreenshotFormatPng, Clip: &page.Viewport{Width: 512, Height: 512, Scale: 1}})
		if err != nil || len(screenshot.Data) == 0 {
			return Frames{}, Unsupported()
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame%04d.png", index)), screenshot.Data, 0600); err != nil {
			return Frames{}, ErrAsset
		}
		result.Durations = append(result.Durations, math.Round(float64(index+1)*outputDuration*1000/float64(count))-math.Round(float64(index)*outputDuration*1000/float64(count)))
	}
	return result, nil
}
