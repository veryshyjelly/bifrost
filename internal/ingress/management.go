package ingress

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"math"
	"sort"
	"strconv"

	"github.com/veryshyjelly/bifrost/internal/delivery"
)

// The existing management protocol signs JSON.stringify({...bodyWithoutSignature}).
// A Go map sorts property names, which would invalidate those existing signatures.
// Keep insertion order, JS integer-key ordering and JS number/string formatting.
type member struct {
	key   string
	value any
}
type orderedObject []member

func parseValue(d *json.Decoder) (any, error) {
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch t {
	case json.Delim('{'):
		out := orderedObject{}
		positions := map[string]int{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			value, err := parseValue(d)
			if err != nil {
				return nil, err
			}
			name := key.(string)
			if pos, found := positions[name]; found {
				out[pos].value = value
			} else {
				positions[name] = len(out)
				out = append(out, member{name, value})
			}
		}
		if _, err := d.Token(); err != nil {
			return nil, err
		}
		return out, nil
	case json.Delim('['):
		out := []any{}
		for d.More() {
			v, err := parseValue(d)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		if _, err := d.Token(); err != nil {
			return nil, err
		}
		return out, nil
	default:
		return t, nil
	}
}

func quote(out []byte, text string) []byte {
	const hex = "0123456789abcdef"
	out = append(out, '"')
	for _, r := range text {
		switch r {
		case '"', '\\':
			out = append(out, '\\', byte(r))
		case '\b':
			out = append(out, '\\', 'b')
		case '\t':
			out = append(out, '\\', 't')
		case '\n':
			out = append(out, '\\', 'n')
		case '\f':
			out = append(out, '\\', 'f')
		case '\r':
			out = append(out, '\\', 'r')
		default:
			if r < 32 {
				out = append(out, '\\', 'u', '0', '0', hex[r>>4], hex[r&15])
			} else {
				out = append(out, string(r)...)
			}
		}
	}
	return append(out, '"')
}

func indexKey(key string) (uint64, bool) {
	i, err := strconv.ParseUint(key, 10, 32)
	return i, err == nil && i < math.MaxUint32 && strconv.FormatUint(i, 10) == key
}

func stringify(out []byte, v any) []byte {
	switch v := v.(type) {
	case orderedObject:
		members := append(orderedObject(nil), v...)
		sort.SliceStable(members, func(i, j int) bool {
			a, ai := indexKey(members[i].key)
			b, bi := indexKey(members[j].key)
			if ai != bi {
				return ai
			}
			return ai && a < b
		})
		out = append(out, '{')
		for i, m := range members {
			if i > 0 {
				out = append(out, ',')
			}
			out = quote(out, m.key)
			out = append(out, ':')
			out = stringify(out, m.value)
		}
		return append(out, '}')
	case []any:
		out = append(out, '[')
		for i, x := range v {
			if i > 0 {
				out = append(out, ',')
			}
			out = stringify(out, x)
		}
		return append(out, ']')
	case string:
		return quote(out, v)
	case json.Number:
		n, _ := strconv.ParseFloat(string(v), 64)
		if math.IsInf(n, 0) || math.IsNaN(n) {
			return append(out, "null"...)
		}
		if n == 0 {
			return append(out, '0')
		}
		format := byte('f')
		if math.Abs(n) < 1e-6 || math.Abs(n) >= 1e21 {
			format = 'e'
		}
		value := strconv.AppendFloat(nil, n, format, -1, 64)
		if at := bytes.IndexByte(value, 'e'); at >= 0 {
			pos := at + 1
			if value[pos] == '+' || value[pos] == '-' {
				pos++
			}
			for len(value)-pos > 1 && value[pos] == '0' {
				value = append(value[:pos], value[pos+1:]...)
			}
		}
		return append(out, value...)
	case bool:
		if v {
			return append(out, "true"...)
		}
		return append(out, "false"...)
	default:
		return append(out, "null"...)
	}
}

func (s *Service) management(ctx context.Context, raw []byte) (bool, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := parseValue(d)
	if err != nil {
		return false, invalid()
	}
	members, ok := v.(orderedObject)
	if !ok {
		return false, invalid()
	}
	if _, err = d.Token(); err != io.EOF {
		return false, invalid()
	}
	unsigned := orderedObject{}
	signature := ""
	for _, m := range members {
		if m.key == "signature" {
			signature, _ = m.value.(string)
		} else {
			unsigned = append(unsigned, m)
		}
	}
	canonical := stringify(nil, unsigned)
	if signature == "" || subtle.ConstantTimeCompare([]byte(signature), []byte(TelegramSecret(string(canonical), s.c.AdminToken))) != 1 {
		return false, &refusal{401, "Invalid management signature"}
	}
	body := object(canonical)
	at, ok := integer(body["at"])
	now := s.c.Now().UnixMilli()
	const window = int64(7 * 24 * 60 * 60 * 1000)
	if !ok || at < now-window || at > now+window {
		return false, &refusal{401, "Invalid management timestamp"}
	}
	id := text(body["id"])
	if id == "" || text(body["type"]) == "" {
		return false, invalid()
	}
	return s.s.Admit(ctx, &delivery.Event{AccountID: s.c.AccountID, Dedup: "management:" + id, Stream: "management:" + id, Kind: "management", Payload: canonical}, nil)
}
