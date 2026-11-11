package telegram

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func TestNativeMetadataSettersRetainEmptyDescriptionsAndFullWidthIDs(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.Contains(r.Header.Get("Content-Type"), "multipart/") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
		} else if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		switch method {
		case "editForumTopic":
			if r.FormValue("chat_id") != "-1001234567890" || r.FormValue("message_thread_id") != "44" || r.FormValue("name") != "😀 Group" {
				t.Error(r.Form)
			}
		case "setChatTitle":
			if r.FormValue("chat_id") != "-1001234567890" || r.FormValue("title") != "Group" {
				t.Error(r.Form)
			}
		case "setChatDescription":
			if _, ok := r.MultipartForm.Value["description"]; !ok || r.FormValue("description") != "" {
				t.Error("empty description omitted", r.Form)
			}
		case "setMyName":
			if r.FormValue("name") != "Friend" {
				t.Error(r.Form)
			}
		case "setMyDescription":
			if r.FormValue("description") != "About" {
				t.Error(r.Form)
			}
		case "getMyDescription":
			fmt.Fprint(w, `{"ok":true,"result":{"description":"Existing"}}`)
			return
		default:
			t.Error("unexpected call", method)
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()
	c, err := New(server.URL, "fixture-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		c.EditForumTopic(t.Context(), &bot.EditForumTopicParams{ChatID: int64(-1001234567890), MessageThreadID: 44, Name: "😀 Group"}),
		c.SetChatTitle(t.Context(), &bot.SetChatTitleParams{ChatID: int64(-1001234567890), Title: "Group"}),
		c.SetChatDescription(t.Context(), &bot.SetChatDescriptionParams{ChatID: int64(-1001234567890), Description: ""}),
		c.SetMyName(t.Context(), &bot.SetMyNameParams{Name: "Friend"}),
		c.SetMyDescription(t.Context(), &bot.SetMyDescriptionParams{Description: "About"}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	current, err := c.MyDescription(t.Context())
	if err != nil || current.Description != "Existing" {
		t.Fatal(current, err)
	}
	if calls != 6 {
		t.Fatal("hidden retry", calls)
	}
}

func TestMetadataAlreadySetIsSuccessButPermissionAndRateRefusalsRemainVisible(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		wantErr    bool
		retry      time.Duration
	}{
		{name: "unchanged title", body: `{"ok":false,"error_code":400,"description":"Bad Request: chat is not modified"}`},
		{name: "unchanged topic", body: `{"ok":false,"error_code":400,"description":"TOPIC_NOT_MODIFIED"}`},
		{name: "permission", body: `{"ok":false,"error_code":403,"description":"Forbidden"}`, wantErr: true},
		{name: "other bad request", body: `{"ok":false,"error_code":400,"description":"Bad Request"}`, wantErr: true},
		{name: "rate", body: `{"ok":false,"error_code":429,"parameters":{"retry_after":23}}`, wantErr: true, retry: 23 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, tc.body) }))
			defer server.Close()
			c, err := New(server.URL, "fixture-token", nil)
			if err != nil {
				t.Fatal(err)
			}
			err = c.SetChatTitle(t.Context(), &bot.SetChatTitleParams{ChatID: int64(-200), Title: "Group"})
			if (err != nil) != tc.wantErr {
				t.Fatal(err)
			}
			if tc.retry > 0 {
				var e *providers.Error
				if !errors.As(err, &e) || e.RetryAfter != tc.retry {
					t.Fatal(err)
				}
			}
			if calls != 1 {
				t.Fatal("hidden retry", calls)
			}
		})
	}
}
