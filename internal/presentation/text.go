// Package presentation renders literal text with native Telegram entities. All
// offsets and budgets use UTF-16 units, never UTF-8 byte offsets.
package presentation

import (
	"errors"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // History timezone rendering also works in the minimal image.
	"unicode"
	"unicode/utf8"

	"github.com/go-telegram/bot/models"
)

var ErrInvalid = errors.New("invalid formatted text or entity boundary")

// Value carries literal text and native Telegram entities with UTF-16 offsets.
type Value struct {
	Text     string                 `json:"text"`
	Entities []models.MessageEntity `json:"entities"`
}

// Options freezes sender labels and history/status context used to compose a relay header.
type Options struct {
	Bot, Sender, Name                     string
	Own, Historical, Status, EditedNotice bool
	HistoryStyle, TimeZone                string
	Timestamp                             int64
	Now                                   time.Time
}

// Length returns the number of UTF-16 code units used by Telegram entity offsets and limits.
func Length(text string) int {
	n := 0
	for _, r := range text {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}
func Plain(text string) Value { return Value{Text: text, Entities: []models.MessageEntity{}} }
func Styled(text string, kind models.MessageEntityType) Value {
	v := Plain(text)
	if text != "" {
		v.Entities = append(v.Entities, models.MessageEntity{Type: kind, Length: Length(text)})
	}
	return v
}

// Concat joins values and shifts entity offsets by preceding UTF-16 text lengths.
func Concat(values ...Value) Value {
	out := Plain("")
	offset := 0
	var text strings.Builder
	for _, v := range values {
		for _, e := range v.Entities {
			e.Offset += offset
			out.Entities = append(out.Entities, e)
		}
		text.WriteString(v.Text)
		offset += Length(v.Text)
	}
	out.Text = text.String()
	return out
}

var phone = regexp.MustCompile(`^\+?([1-9][0-9]{4,14})@(?:c\.us|s\.whatsapp\.net)$`)
var nameBreaks = regexp.MustCompile(`[\r\n\t]+`)

func WhatsAppURL(id string) string {
	m := phone.FindStringSubmatch(id)
	if m != nil {
		return "https://wa.me/" + m[1]
	}
	return "https://wa.me/"
}
func trimJS(text string) string {
	return strings.TrimFunc(text, func(r rune) bool { return r == 0xfeff || (r != 0x85 && unicode.IsSpace(r)) })
}
func Sender(name, id string) Value {
	v := Styled(trimJS(nameBreaks.ReplaceAllString(name, " ")), models.MessageEntityTypeBold)
	if v.Text != "" {
		v.Entities = append(v.Entities, models.MessageEntity{Type: models.MessageEntityTypeTextLink, Length: Length(v.Text), URL: WhatsAppURL(id)})
	}
	return v
}

func validTime(unix int64, now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	return unix > 0 && unix <= now.Unix()+1098*86400
}
func nativeTime(text, format string, unix int64) Value {
	v := Styled(text, models.MessageEntityTypeDateTime)
	v.Entities[0].UnixTime = int(unix)
	v.Entities[0].DateTimeFormat = format
	return v
}
func HistoryTime(unix int64, label string, now time.Time) Value {
	if label == "" {
		label = "History"
	}
	if !validTime(unix, now) {
		return Styled(label, models.MessageEntityTypeItalic)
	}
	return Concat(Styled(label, models.MessageEntityTypeItalic), Plain(" · "), nativeTime(time.Unix(unix, 0).UTC().Format("2006-01-02 15:04 UTC"), "dt", unix))
}
func HistoryClock(unix int64, zone string, now time.Time) (Value, error) {
	if !validTime(unix, now) {
		return Styled("time unknown", models.MessageEntityTypeItalic), nil
	}
	if zone == "" {
		zone = "UTC"
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return Value{}, ErrInvalid
	}
	return nativeTime(time.Unix(unix, 0).In(location).Format("15:04"), "t", unix), nil
}
func HistoryDay(unix int64, zone string, now time.Time) (string, Value, error) {
	if !validTime(unix, now) {
		return "unknown", Styled("Date unknown", models.MessageEntityTypeBold), nil
	}
	if zone == "" {
		zone = "UTC"
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return "", Value{}, ErrInvalid
	}
	date := time.Unix(unix, 0).In(location)
	v := nativeTime(date.Format("2 January 2006"), "D", unix)
	v.Entities = append(v.Entities, models.MessageEntity{Type: models.MessageEntityTypeBold, Length: Length(v.Text)})
	return date.Format("2006-01-02"), v, nil
}

func Header(o Options) (Value, error) {
	if o.Historical && !o.Status && o.HistoryStyle == "compact" {
		name := Plain("")
		if o.Own {
			name = Sender("You", o.Sender)
		} else if o.Bot == "master" {
			name = Sender(o.Name, o.Sender)
		}
		clock, err := HistoryClock(o.Timestamp, o.TimeZone, o.Now)
		if err != nil {
			return Value{}, err
		}
		left := "["
		if name.Text != "" {
			left = " ["
		}
		v := Concat(name, Plain(left), clock, Plain("]"))
		if o.EditedNotice {
			v = Concat(Styled("Edited", models.MessageEntityTypeItalic), Plain(" · "), v)
		}
		return v, nil
	}
	parts := []Value{}
	if o.EditedNotice {
		parts = append(parts, Styled("Edited", models.MessageEntityTypeItalic))
	}
	if o.Status {
		parts = append(parts, HistoryTime(o.Timestamp, "Status", o.Now))
	} else if o.Historical {
		parts = append(parts, HistoryTime(o.Timestamp, "History", o.Now))
	}
	if o.Own {
		parts = append(parts, Concat(Sender("You", o.Sender), Plain(" · "), Styled("sent from WhatsApp", models.MessageEntityTypeItalic)))
	} else if o.Bot == "master" {
		parts = append(parts, Sender(o.Name, o.Sender))
	}
	result := Plain("")
	for _, p := range parts {
		if result.Text != "" {
			result = Concat(result, Plain(" · "), p)
		} else {
			result = p
		}
	}
	return result, nil
}
func Relay(o Options, body Value) (Value, error) {
	header, err := Header(o)
	if err != nil {
		return Value{}, err
	}
	if header.Text == "" {
		return body, nil
	}
	if body.Text == "" {
		return header, nil
	}
	separator := " · "
	if o.Own || o.Bot == "master" || (o.Historical && !o.Status && o.HistoryStyle == "compact") {
		separator = ": "
	}
	return Concat(header, Plain(separator), body), nil
}

// indices maps UTF-16 boundaries to bytes; the middle of a surrogate pair is -1.
func indices(text string) ([]int, error) {
	if !utf8.ValidString(text) {
		return nil, ErrInvalid
	}
	out := make([]int, 0, Length(text)+1)
	for i, r := range text {
		out = append(out, i)
		if r > 0xffff {
			out = append(out, -1)
		}
	}
	return append(out, len(text)), nil
}
func validate(v Value, index []int) error {
	for _, e := range v.Entities {
		if e.Offset < 0 || e.Length <= 0 || e.Offset >= len(index) || e.Length > len(index)-1-e.Offset || index[e.Offset] < 0 || index[e.Offset+e.Length] < 0 {
			return ErrInvalid
		}
	}
	return nil
}
func slice(v Value, start, end int, index []int) Value {
	out := Plain(v.Text[index[start]:index[end]])
	for _, e := range v.Entities {
		left, right := max(start, e.Offset), min(end, e.Offset+e.Length)
		if right <= left || (e.Type == models.MessageEntityTypeDateTime && (left != e.Offset || right != e.Offset+e.Length)) {
			continue
		}
		e.Offset = left - start
		e.Length = right - left
		out.Entities = append(out.Entities, e)
	}
	return out
}

// Slice extracts the half-open UTF-16 range [start, end), rejecting invalid input entities or rune boundaries.
func Slice(v Value, start, end int) (Value, error) {
	index, err := indices(v.Text)
	if err != nil {
		return Value{}, err
	}
	if start < 0 || end < start || end >= len(index) || index[start] < 0 || index[end] < 0 || validate(v, index) != nil {
		return Value{}, ErrInvalid
	}
	return slice(v, start, end, index), nil
}

// Split partitions text within a UTF-16 budget while preserving rune boundaries and whole date-time entities.
// It rejects a budget too small to hold the next indivisible fragment.
func Split(v Value, limit int) ([]Value, error) {
	if limit <= 0 {
		return nil, ErrInvalid
	}
	index, err := indices(v.Text)
	if err != nil || validate(v, index) != nil {
		return nil, ErrInvalid
	}
	out := []Value{}
	for start := 0; start < len(index)-1; {
		end := min(len(index)-1, start+limit)
		if index[end] < 0 {
			end--
		}
		for _, e := range v.Entities {
			if e.Type == models.MessageEntityTypeDateTime && e.Offset < end && e.Offset+e.Length > end {
				end = e.Offset
			}
		}
		if end <= start {
			return nil, ErrInvalid
		}
		out = append(out, slice(v, start, end, index))
		start = end
	}
	if len(out) == 0 {
		out = append(out, Plain(""))
	}
	return out, nil
}
