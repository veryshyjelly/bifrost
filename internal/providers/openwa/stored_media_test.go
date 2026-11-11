package openwa

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/providers"
)

func TestStoredMediaUsesNativeSDKAndRetainsOriginalHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/sessions/fixture/messages/peer@g.us/message/media" || r.Header.Get("X-API-Key") != "fixture-key" {
			t.Error("wrong stored-media request")
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Media-Mimetype", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename*=UTF-8''original%20file.pdf`)
		fmt.Fprint(w, "fixture document")
	}))
	defer server.Close()
	client, err := New(server.URL, "fixture-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	media, err := client.StoredMessageMedia(t.Context(), "fixture", "peer@g.us", "message", 1024)
	if err != nil || string(media.Data) != "fixture document" || media.MIME != "application/pdf" || media.Filename != "original file.pdf" {
		t.Fatal(media.MIME, media.Filename, err)
	}
}
func TestStoredMediaBoundsDeclaredAndChunkedSDKResponses(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		t.Run(fmt.Sprint(chunked), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/octet-stream")
				if chunked {
					w.(http.Flusher).Flush()
				}
				w.Write(bytes.Repeat([]byte{42}, 1025))
			}))
			defer server.Close()
			client, _ := New(server.URL, "fixture-key", nil)
			_, err := client.StoredMessageMedia(t.Context(), "fixture", "peer@g.us", "message", 1024)
			var refusal *providers.Error
			if !errors.As(err, &refusal) || refusal.Status != 413 {
				t.Fatal(err)
			}
		})
	}
}
