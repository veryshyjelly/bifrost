// Package statuses archives contact stories without sending WhatsApp read/view
// receipts or exposing a publication capability.
package statuses

import (
	"encoding/json"
	"strconv"
	"time"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/identity"
)

const ChatID = "status@broadcast"

type Post struct {
	ID, Author, Name, Kind, Body string
	PostedAt, ExpiresAt          int64
	HasMedia, MediaOmitted       bool
}
type Feed struct{ Management, ThreadID int64 }
type Mute struct {
	Author, Name string
	Active       bool
	Revision     int64
}
type Settings struct {
	Session                           string
	OwnerID, Management               int64
	CommandTTL, NoticeTTL, ControlTTL int
}

func millis(raw json.RawMessage) int64 {
	var n json.Number
	if len(raw) > 0 && raw[0] != '"' && json.Unmarshal(raw, &n) == nil {
		v, err := strconv.ParseFloat(string(n), 64)
		if err == nil && v > 0 && v <= 9007199254740991 && float64(int64(v)) == v {
			return int64(v)
		}
		return 0
	}
	var date string
	if json.Unmarshal(raw, &date) == nil {
		if parsed, err := time.Parse(time.RFC3339Nano, date); err == nil && parsed.UnixMilli() > 0 {
			return parsed.UnixMilli()
		}
	}
	return 0
}

// Decode accepts only the authenticated status.received shape. External media
// URLs are deliberately absent from the frozen domain value.
func Decode(raw json.RawMessage, now time.Time) (Post, bool) {
	var input struct {
		StatusID                       string            `json:"statusId"`
		Contact                        sdk.StatusContact `json:"contact"`
		Type, Caption                  string
		PostedAt, Timestamp, ExpiresAt json.RawMessage
		HasMedia                       *bool
		MediaOmitted                   bool
	}
	if json.Unmarshal(raw, &input) != nil || input.StatusID == "" || !identity.IsPerson(input.Contact.ID) {
		return Post{}, false
	}
	posted := millis(input.PostedAt)
	if len(input.PostedAt) == 0 || string(input.PostedAt) == "null" {
		posted = millis(input.Timestamp)
	}
	if posted <= 0 || posted > 9007199254740991-int64(24*time.Hour/time.Millisecond) {
		return Post{}, false
	}
	expires := millis(input.ExpiresAt)
	if expires == 0 {
		expires = posted + int64(24*time.Hour/time.Millisecond)
	}
	if expires <= now.UnixMilli() {
		return Post{}, false
	}
	p := Post{ID: input.StatusID, Author: input.Contact.ID, Name: input.Contact.Name, Kind: input.Type, Body: input.Caption, PostedAt: posted, ExpiresAt: expires, MediaOmitted: input.MediaOmitted}
	if p.Name == "" {
		p.Name = input.Contact.PushName
	}
	if p.Name == "" {
		p.Name = p.Author
	}
	if input.HasMedia != nil {
		p.HasMedia = *input.HasMedia
		p.MediaOmitted = p.MediaOmitted || !*input.HasMedia
	}
	switch p.Kind {
	case "text", "image", "video", "voice":
	default:
		p.Kind = "text"
		label := input.Type
		if label == "" {
			label = "Unsupported"
		}
		p.Body += "\n[" + label + " status]"
	}
	return p, true
}

func FromRecord(r sdk.StatusRecord, now time.Time) (Post, bool) {
	raw, _ := json.Marshal(map[string]any{"statusId": r.ID, "contact": r.Contact, "type": r.Type, "caption": r.Caption, "postedAt": r.Timestamp.UnixMilli(), "expiresAt": r.ExpiresAt, "hasMedia": r.MediaURL != ""})
	return Decode(raw, now)
}

func (p Post) Message() sdk.ChatHistoryMessage {
	kind := sdk.MsgText
	switch p.Kind {
	case "image":
		kind = sdk.MsgImage
	case "video":
		kind = sdk.MsgVideo
	case "voice":
		kind = sdk.MsgVoice
	}
	m := sdk.ChatHistoryMessage{ID: p.ID, ChatID: ChatID, Author: p.Author, From: p.Author, Body: p.Body, Type: kind, Kind: "status", IsStatusBroadcast: true, Timestamp: p.PostedAt / 1000}
	if kind != sdk.MsgText {
		m.Media = &sdk.ChatHistoryMedia{Omitted: p.MediaOmitted}
	}
	return m
}

func (p Post) Source(canonical string) identity.Source {
	if canonical == "" {
		canonical = p.Author
	}
	return identity.Source{ChatID: ChatID, RawChatID: ChatID, MessageID: p.ID, SenderID: canonical, LinkSenderID: canonical, Name: p.Name, Body: p.Body}
}
