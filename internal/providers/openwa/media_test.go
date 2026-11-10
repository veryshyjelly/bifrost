package openwa

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/providers"
)

func TestSourceMediaStreamsBeyondJSONLimitWithMetadataAndCap(t *testing.T) {
	const size = 9 << 20
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.HasSuffix(r.URL.Path, "/media/download") || r.Method != "GET" || r.URL.Query().Get("maxBytes") != "10485760" {
			t.Error("wrong source-media operation")
		}
		w.Header().Set("Content-Length", fmt.Sprint(size))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Media-Mimetype", "application/pdf")
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''r%C3%A9sum%C3%A9.pdf")
		_, _ = io.Copy(w, strings.NewReader(strings.Repeat("x", size)))
	}))
	defer server.Close()
	client, _ := New(server.URL, "fixture-key", nil)
	media, err := client.SourceMedia(t.Context(), "session", "chat@g.us", "message", 10<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer media.Body.Close()
	n, err := io.Copy(io.Discard, media.Body)
	if err != nil || n != size || media.Mimetype != "application/pdf" || media.Filename != "résumé.pdf" {
		t.Fatalf("stream bytes=%d error=%v", n, err)
	}
	if calls.Load() != 1 {
		t.Fatal("source read caused history, read-receipt or archive requests")
	}
}

func TestSourceMediaRejectsOversizeAndRedactsInterruptedReads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.(http.Flusher).Flush(); fmt.Fprint(w, "12345678") }))
	defer server.Close()
	client, _ := New(server.URL, "fixture-key", nil)
	media, err := client.SourceMedia(t.Context(), "s", "c", "m", 4)
	if err != nil {
		t.Fatal(err)
	}
	defer media.Body.Close()
	data, err := io.ReadAll(media.Body)
	var outcome *providers.Error
	if !errors.As(err, &outcome) || outcome.Code != "MEDIA_TOO_LARGE" || outcome.Status != 413 || len(data) > 4 {
		t.Fatalf("oversize read: %v", err)
	}
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.(http.Flusher).Flush(); <-r.Context().Done() }))
	defer stalled.Close()
	client, _ = New(stalled.URL, "fixture-key", nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	media, err = client.SourceMedia(ctx, "s", "c", "m", 16)
	if err != nil {
		t.Fatal(err)
	}
	defer media.Body.Close()
	cancel()
	_, err = io.ReadAll(media.Body)
	if !errors.As(err, &outcome) || outcome.Code != "MEDIA_READ_INTERRUPTED" || !outcome.Retry || outcome.Uncertain {
		t.Fatalf("interrupted read: %v", err)
	}
}

func TestSourceMediaDistinguishesMissingSourceFromMissingRouteAndRetry(t *testing.T) {
	for _, tc := range []struct {
		name, body, code string
		status           int
		retry            bool
	}{
		{"source missing", `{"code":"MEDIA_SOURCE_UNAVAILABLE","message":"private detail"}`, "MEDIA_SOURCE_UNAVAILABLE", 404, false},
		{"stored miss", `{"code":"MEDIA_NOT_STORED","message":"private detail"}`, "MEDIA_NOT_STORED", 404, false},
		{"missing route", `{"message":"private detail"}`, "MEDIA_ROUTE_UNAVAILABLE", 404, true},
		{"unavailable", `{"message":"private detail"}`, "OUTCOME_UNKNOWN", 503, true},
		{"forbidden", `{"message":"private detail"}`, "REFUSED", 403, false},
		{"disabled", `{"code":"MEDIA_DOWNLOAD_DISABLED","message":"private detail"}`, "MEDIA_DOWNLOAD_DISABLED", 409, false},
		{"too large", `{"message":"private detail"}`, "MEDIA_TOO_LARGE", 413, false},
		{"unsupported", `{"message":"private detail"}`, "MEDIA_UNSUPPORTED", 501, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer server.Close()
			client, _ := New(server.URL, "fixture-key", nil)
			_, err := client.SourceMedia(t.Context(), "s", "c", "m", 1)
			var outcome *providers.Error
			if !errors.As(err, &outcome) || outcome.Code != tc.code || outcome.Retry != tc.retry || outcome.Uncertain {
				t.Fatalf("source result: %+v", err)
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatal("private error body escaped")
			}
		})
	}
}
