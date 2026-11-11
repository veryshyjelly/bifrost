package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/veryshyjelly/bifrost/internal/providers"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const MaxPictureBytes int64 = 8 << 20

type PictureDownloader interface {
	Download(context.Context, string, int64) ([]byte, error)
}
type CDN struct{ client *http.Client }

func NewCDN(transport http.RoundTripper) *CDN {
	if transport == nil {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	return &CDN{client: &http.Client{Transport: transport, Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func PictureURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.Hostname() == "" || u.Port() != "" && u.Port() != "443" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range []string{"whatsapp.net", "whatsapp.com", "fbcdn.net"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func (d *CDN) Download(ctx context.Context, raw string, max int64) ([]byte, error) {
	if !PictureURL(raw) || max <= 0 || max > MaxPictureBytes {
		return nil, &providers.Error{Service: "picture", Status: 400, Code: "INVALID_PICTURE_REQUEST"}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, &providers.Error{Service: "picture", Code: "INVALID_PICTURE_REQUEST"}
	}
	response, err := d.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &providers.Error{Service: "picture", Code: "PICTURE_UNREACHABLE", Retry: true}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		delay := 5 * time.Second
		if seconds, err := strconv.ParseUint(response.Header.Get("Retry-After"), 10, 32); err == nil {
			delay = time.Duration(seconds) * time.Second
		} else if date, err := http.ParseTime(response.Header.Get("Retry-After")); err == nil && date.After(time.Now()) {
			delay = time.Until(date)
		}
		return nil, &providers.Error{Service: "picture", Status: response.StatusCode, Code: "PICTURE_REFUSED", Retry: response.StatusCode >= 300 && response.StatusCode < 400 || response.StatusCode == 408 || response.StatusCode == 409 || response.StatusCode == 429 || response.StatusCode >= 500, RetryAfter: delay}
	}
	if response.ContentLength > max {
		return nil, pictureTooLarge()
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, max+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &providers.Error{Service: "picture", Code: "PICTURE_INTERRUPTED", Retry: true}
	}
	if int64(len(data)) > max {
		return nil, pictureTooLarge()
	}
	return data, nil
}

func pictureTooLarge() error {
	return &providers.Error{Service: "picture", Status: 413, Code: "PICTURE_TOO_LARGE"}
}
func PictureHash(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

// PictureJPEG validates dimensions before allocating the decoded image. JPEG
// sources retain their bytes; other supported static images use their first
// frame. The upload stays within the caller's configured byte budget.
func PictureJPEG(data []byte, max int64) ([]byte, error) {
	if len(data) == 0 || int64(len(data)) > max || max <= 0 || max > MaxPictureBytes {
		return nil, pictureTooLarge()
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 16<<20 {
		return nil, &providers.Error{Service: "picture", Status: 415, Code: "INVALID_PICTURE"}
	}
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, &providers.Error{Service: "picture", Status: 415, Code: "INVALID_PICTURE"}
	}
	if format == "jpeg" {
		return append([]byte(nil), data...), nil
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, decoded, &jpeg.Options{Quality: 85}); err != nil {
		return nil, &providers.Error{Service: "picture", Status: 415, Code: "PICTURE_CONVERSION_FAILED"}
	}
	if int64(b.Len()) > max {
		return nil, pictureTooLarge()
	}
	return b.Bytes(), nil
}
