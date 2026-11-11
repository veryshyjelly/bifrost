package telegram

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func TestNativePictureMultipartUploadsAndUniqueIDs(t *testing.T) {
	data := []byte("complete-fixture-image")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		var field string
		switch {
		case strings.HasSuffix(r.URL.Path, "setChatPhoto"):
			if r.FormValue("chat_id") != "-1001234567890" {
				t.Error("chat ID lost", r.Form)
			}
			field = "photo"
		case strings.HasSuffix(r.URL.Path, "setMyProfilePhoto"):
			var photo struct{ Type, Photo string }
			if err := json.Unmarshal([]byte(r.FormValue("photo")), &photo); err != nil || photo.Type != "static" || photo.Photo != "attach://image" {
				t.Error("native static photo lost", photo, err)
			}
			field = "image"
		case strings.HasSuffix(r.URL.Path, "getUserProfilePhotos"):
			if r.FormValue("user_id") != "9007199254740991" || r.FormValue("limit") != "1" {
				t.Error(r.Form)
			}
			fmt.Fprint(w, `{"ok":true,"result":{"total_count":1,"photos":[[{"file_id":"file","file_unique_id":"unique","width":640,"height":640}]]}}`)
			return
		default:
			t.Error("unexpected native method", r.URL.Path)
			return
		}
		f, _, err := r.FormFile(field)
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		got, err := io.ReadAll(f)
		if err != nil || !bytes.Equal(got, data) {
			t.Error("multipart bytes changed", got, err)
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()
	c, err := New(server.URL, "fixture-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetChatPhoto(t.Context(), &bot.SetChatPhotoParams{ChatID: int64(-1001234567890), Photo: &models.InputFileUpload{Filename: "profile.jpg", Data: bytes.NewReader(data)}}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetMyProfilePhoto(t.Context(), &bot.SetMyProfilePhotoParams{Photo: &models.InputProfilePhotoStatic{Photo: "attach://image", MediaAttachment: bytes.NewReader(data)}}); err != nil {
		t.Fatal(err)
	}
	photos, err := c.ProfilePhotos(t.Context(), &bot.GetUserProfilePhotosParams{UserID: 9007199254740991, Limit: 1})
	if err != nil || photos.Photos[0][0].FileUniqueID != "unique" {
		t.Fatal(photos, err)
	}
	if calls.Load() != 3 {
		t.Fatal("hidden retry", calls.Load())
	}
}

func TestNativePictureUploadsRetainUncertaintyAndRateRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, body             string
		drop, uncertain, retry bool
	}{
		{name: "lost response", drop: true, uncertain: true},
		{name: "false result", body: `{"ok":true,"result":false}`, uncertain: true},
		{name: "provider 503", body: `{"ok":false,"error_code":503}`, uncertain: true},
		{name: "rate", body: `{"ok":false,"error_code":429,"parameters":{"retry_after":9}}`, retry: true},
		{name: "permission", body: `{"ok":false,"error_code":403}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if tc.drop {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
					} else {
						conn.Close()
					}
					return
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			c, err := New(server.URL, "fixture-token", nil)
			if err != nil {
				t.Fatal(err)
			}
			err = c.SetChatPhoto(t.Context(), &bot.SetChatPhotoParams{ChatID: int64(-200), Photo: &models.InputFileUpload{Filename: "profile.jpg", Data: strings.NewReader("image")}})
			var e *providers.Error
			if !errors.As(err, &e) || e.Uncertain != tc.uncertain || e.Retry != tc.retry || tc.retry && e.RetryAfter.Seconds() != 9 {
				t.Fatal(err)
			}
			if calls.Load() != 1 {
				t.Fatal("photo upload retried automatically", calls.Load())
			}
		})
	}
}
