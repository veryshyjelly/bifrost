package metadata

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/providers"
)

type pictureTransport func(*http.Request) (*http.Response, error)

func (f pictureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPictureCDNBoundaryDoesNotLeakCredentialsOrFollowRedirects(t *testing.T) {
	for _, url := range []string{"http://mmg.whatsapp.net/a", "https://whatsapp.net.evil.test/a", "https://evilwhatsapp.net/a", "https://user:secret@whatsapp.net/a", "https://whatsapp.net:444/a", "https://127.0.0.1/a", "https://whatsapp.net/a#fragment"} {
		calls := 0
		d := NewCDN(pictureTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected") }))
		if _, err := d.Download(t.Context(), url, 1024); err == nil || calls != 0 {
			t.Fatal("invalid CDN accepted", url, calls, err)
		}
	}
	calls := 0
	d := NewCDN(pictureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.Header.Get("Authorization") != "" || r.Header.Get("X-API-Key") != "" {
			t.Error("credentials or mutation leaked")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.test/"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}))
	_, err := d.Download(t.Context(), "https://mmg.whatsapp.net/a?token=private-fixture", 1024)
	if err == nil || strings.Contains(err.Error(), "private-fixture") || calls != 1 {
		t.Fatal("redirect or URL leak", calls, err)
	}
	for _, url := range []string{"https://WHATSAPP.NET/a", "https://a.whatsapp.com/a", "https://a.fbcdn.net/a", "https://mmg.whatsapp.net:443/a"} {
		if !PictureURL(url) {
			t.Fatal("supported CDN rejected", url)
		}
	}
}

type interruptedPicture struct{}

func (interruptedPicture) Read(p []byte) (int, error) {
	copy(p, "partial")
	return 7, io.ErrUnexpectedEOF
}
func (interruptedPicture) Close() error { return nil }

func TestPictureCDNEnforcesActualByteCapAndRetainsRetryDelays(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		length     int64
		body       io.ReadCloser
		retry      string
		wantStatus int
	}{
		{name: "declared size", status: 200, length: 2000, body: io.NopCloser(strings.NewReader("small")), wantStatus: 413},
		{name: "chunked size", status: 200, length: -1, body: io.NopCloser(strings.NewReader(strings.Repeat("x", 1025))), wantStatus: 413},
		{name: "partial", status: 200, length: -1, body: interruptedPicture{}},
		{name: "rate", status: 429, body: io.NopCloser(strings.NewReader("")), retry: "17", wantStatus: 429},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewCDN(pictureTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, ContentLength: tc.length, Header: http.Header{"Retry-After": []string{tc.retry}}, Body: tc.body, Request: r}, nil
			}))
			data, err := d.Download(t.Context(), "https://mmg.whatsapp.net/a", 1024)
			var e *providers.Error
			if data != nil || !errors.As(err, &e) || e.Status != tc.wantStatus {
				t.Fatal("partial/oversize picture retained", data, err)
			}
			if tc.status == 429 && (!e.Retry || e.RetryAfter.Seconds() != 17) {
				t.Fatal(e)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	d := NewCDN(pictureTransport(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() }))
	if _, err := d.Download(ctx, "https://mmg.whatsapp.net/a", 1024); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestPictureJPEGPreservesJPEGAndConvertsSupportedImages(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var pngData, jpegData bytes.Buffer
	if err := png.Encode(&pngData, img); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&jpegData, img, nil); err != nil {
		t.Fatal(err)
	}
	got, err := PictureJPEG(jpegData.Bytes(), MaxPictureBytes)
	if err != nil || !bytes.Equal(got, jpegData.Bytes()) {
		t.Fatal("JPEG altered", err)
	}
	converted, err := PictureJPEG(pngData.Bytes(), MaxPictureBytes)
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(converted))
	if err != nil || format != "jpeg" || config.Width != 2 || config.Height != 2 {
		t.Fatal(config, format, err)
	}
	if _, err := PictureJPEG(pngData.Bytes(), int64(pngData.Len())); err == nil {
		t.Fatal("oversize converted JPEG accepted")
	}
	if _, err := PictureJPEG([]byte("broken"), MaxPictureBytes); err == nil {
		t.Fatal("invalid image accepted")
	}
	bomb := append([]byte(nil), pngData.Bytes()...)
	binary.BigEndian.PutUint32(bomb[20:24], 1<<25)
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	if _, err := PictureJPEG(bomb, MaxPictureBytes); err == nil {
		t.Fatal("oversize decoded image accepted")
	}
}
