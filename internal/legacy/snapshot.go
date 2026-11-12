package legacy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/veryshyjelly/bifrost/internal/identity"
)

var ErrUnsupported = errors.New("legacy state contains an unsupported durable domain or job type")

// Record retains exact SQLite strings, including JSON source strings and
// encrypted credentials. Only one typed row is present. Importers must commit
// no rows until ReadSnapshot succeeds, including its cross-row checks.
type Record struct {
	Table           string
	Ordinal         int64
	Raw             json.RawMessage
	KV              *KV
	Job             *Job
	Effect          *Effect
	Message         *Message
	Alias           *Alias
	TelegramMessage *TelegramMessage
}

type KV struct{ Kind, ID, Value string }
type Job struct {
	ID                                  int64
	Dedup, Stream, Type, Payload, State string
	Attempts                            int32
	Due                                 int64
	Error                               *string
	Created                             int64
}
type Effect struct {
	ID, State string
	Result    *string
}
type Message struct {
	WAChat         string `json:"wa_chat"`
	WAID           string `json:"wa_id"`
	TGChat         string `json:"tg_chat"`
	TGID           int64  `json:"tg_id"`
	Bot, Direction string
	Media          int
}
type Alias struct{ Alias, Canonical string }
type TelegramMessage struct {
	Seq           int64
	Scope, Chat   string
	Message       int64
	Bot           string
	Thread        int64
	Kind          string
	Date, Expires int64
	Deleted       int
	Error         *string
}

type SnapshotReport struct {
	Rows         map[string]int64 `json:"rows"`
	Domains      map[string]int64 `json:"domains"`
	JobStates    map[string]int64 `json:"job_states"`
	EffectStates map[string]int64 `json:"effect_states"`
	Digest       string           `json:"sqlite_digest"`
}

var durableDomains = stringSet(`action bot chat-request contact control-clear control-index custom-reaction-emoji delivery-target destination-complete destination-start edit-refusal formatted-packets history-choice history-copy history-copy-complete history-day history-plan history-run identity inbox-return latest-edit mention-render mention-token message-presentation migrated outbound-edit outbound-mentions outbound-sticker outgoing-reaction poll-selection provision reaction-echo reaction-self retired-topic revocation revoked route status-feed status-mute status-mute-counter status-mute-op status-skipped sync-group sync-photo sync-result sync-run sync-status sync-waiter telegram-identity telegram-menu telegram-receipt tg-reaction-state wa-complete wa-reaction-state wamessage`)
var durableJobs = stringSet(`wa tg management control-retire-inbox control-reaction-notice control-edit-notice control-expire control-menu control-clear control-revoke history history-message history-finish status-startup status-refresh sync-start sync-target sync-finish`)

func stringSet(text string) map[string]bool {
	set := make(map[string]bool)
	for _, v := range strings.Fields(text) {
		set[v] = true
	}
	return set
}

// ReadSnapshot validates the six legacy tables and identity relationships. It
// preserves sending/running states as source evidence; recovery belongs to an
// explicit, transactional migration step, never to this reader.
func ReadSnapshot(ctx context.Context, b *Bundle, options SQLiteOptions, accept func(Record) error) (SnapshotReport, error) {
	report := SnapshotReport{Rows: map[string]int64{}, Domains: map[string]int64{}, JobStates: map[string]int64{}, EffectStates: map[string]int64{}}
	if b == nil || b.root == "" {
		return report, ErrInvalid
	}
	botIDs := map[string]bool{"master": true}
	botRefs := make(map[string]bool)
	aliases := []identity.Alias{}
	bindings := []identity.Binding{}
	bots := []identity.Bot{}
	mapped := make(map[string]bool)
	var ordinal int64
	info, err := ReadStagedSQLite(ctx, filepath.Join(b.root, "data", "bridge.db"), options, func(row Row) error {
		ordinal++
		record := Record{Table: row.Table, Ordinal: ordinal, Raw: bytes.Clone(row.Values)}
		if validateJSON([]byte(row.Values), false) != nil {
			return ErrInvalid
		}
		switch row.Table {
		case "kv":
			v := new(KV)
			record.KV = v
			if decodeStrictJSON(row.Values, v) != nil || !validText(v.Kind, 128) || !validText(v.ID, 8192) || validateJSON([]byte(v.Value), false) != nil {
				return ErrInvalid
			}
			if !durableDomains[v.Kind] {
				return ErrUnsupported
			}
			report.Domains[v.Kind]++
			switch v.Kind {
			case "bot":
				if b.config.validateBot(v.ID, []byte(v.Value)) != nil {
					return ErrInvalid
				}
				botIDs[v.ID] = true
				bots = append(bots, identity.Bot{ID: v.ID})
			case "contact":
				var contact struct {
					Bot string `json:"bot"`
				}
				if json.Unmarshal([]byte(v.Value), &contact) != nil {
					return ErrInvalid
				}
				if contact.Bot != "" {
					if !identity.IsPerson(v.ID) {
						return ErrInvalid
					}
					botRefs[contact.Bot] = true
					bindings = append(bindings, identity.Binding{PersonID: v.ID, BotID: contact.Bot})
				}
			case "identity":
				var saved struct {
					ID string `json:"id"`
				}
				if v.ID != "account" || decodeStrictJSON([]byte(v.Value), &saved) != nil || saved.ID != b.Account {
					return ErrInvalid
				}
			case "migrated":
				if v.Value != "true" {
					return ErrInvalid
				}
				botRefs[v.ID] = true
			}
		case "jobs":
			v := new(Job)
			record.Job = v
			if decodeStrictJSON(row.Values, v) != nil || v.ID <= 0 || v.ID == math.MaxInt64 || !validText(v.Dedup, 8192) || !validText(v.Stream, 8192) || v.Attempts < 0 || !validMilliseconds(v.Due) || !validMilliseconds(v.Created) || validateJSON([]byte(v.Payload), true) != nil || !validOptional(v.Error) {
				return ErrInvalid
			}
			if !durableJobs[v.Type] {
				return ErrUnsupported
			}
			if v.State != "pending" && v.State != "running" && v.State != "done" && v.State != "failed" && v.State != "uncertain" {
				return ErrInvalid
			}
			report.JobStates[v.State]++
		case "effects":
			v := new(Effect)
			record.Effect = v
			if decodeStrictJSON(row.Values, v) != nil || !validText(v.ID, 16384) || (v.State != "pending" && v.State != "sending" && v.State != "done" && v.State != "uncertain") || (v.Result != nil && validateJSON([]byte(*v.Result), false) != nil) {
				return ErrInvalid
			}
			if v.State == "done" && v.Result == nil {
				return ErrInvalid
			}
			report.EffectStates[v.State]++
		case "messages":
			v := new(Message)
			record.Message = v
			if decodeStrictJSON(row.Values, v) != nil || !validWAChat(v.WAChat) || !validText(v.WAID, 8192) || !validTelegramChat(v.TGChat) || !validMessageID(v.TGID) || !validText(v.Bot, 128) || (v.Direction != "in" && v.Direction != "out") || (v.Media != 0 && v.Media != 1) {
				return ErrInvalid
			}
			botRefs[v.Bot] = true
			scope := v.Bot
			if strings.HasPrefix(v.TGChat, "-") {
				scope = "*"
			}
			key := scope + ":" + v.TGChat + ":" + strconv.FormatInt(v.TGID, 10)
			if mapped[key] {
				return ErrInvalid
			}
			mapped[key] = true
		case "aliases":
			v := new(Alias)
			record.Alias = v
			if decodeStrictJSON(row.Values, v) != nil {
				return ErrInvalid
			}
			aliases = append(aliases, identity.Alias{ID: v.Alias, Canonical: v.Canonical})
		case "telegram_messages":
			v := new(TelegramMessage)
			record.TelegramMessage = v
			if decodeStrictJSON(row.Values, v) != nil || v.Seq <= 0 || v.Seq == math.MaxInt64 || !validTelegramChat(v.Chat) || !validMessageID(v.Message) || v.Thread < 0 || v.Thread > math.MaxInt32 || !validText(v.Bot, 128) || !validText(v.Kind, 128) || !validMilliseconds(v.Date) || !validMilliseconds(v.Expires) || (v.Deleted != 0 && v.Deleted != 1) || !validOptional(v.Error) {
				return ErrInvalid
			}
			expected := v.Bot
			if strings.HasPrefix(v.Chat, "-") {
				expected = "*"
			}
			if v.Scope != expected {
				return ErrInvalid
			}
			botRefs[v.Bot] = true
		default:
			return ErrInvalid
		}
		report.Rows[row.Table]++
		if accept != nil {
			return accept(record)
		}
		return nil
	})
	if err != nil {
		return report, err
	}
	if info.Digest != b.SQLite.Digest {
		return report, ErrInvalid
	}
	for id := range botRefs {
		if !botIDs[id] {
			return report, ErrInvalid
		}
	}
	if _, err := identity.New(aliases, nil, bots, bindings); err != nil {
		return report, ErrInvalid
	}
	for table := range sourceTables {
		if _, ok := report.Rows[table]; !ok {
			report.Rows[table] = 0
		}
	}
	report.Digest = info.Digest
	return report, nil
}

func validText(s string, max int) bool {
	return s != "" && len(s) <= max && !strings.ContainsRune(s, 0)
}
func validOptional(s *string) bool   { return s == nil || !strings.ContainsRune(*s, 0) }
func validMilliseconds(v int64) bool { return v >= 0 && v <= 253402300799999 }
func validMessageID(v int64) bool    { return v > 0 && v <= math.MaxInt32 }
func validTelegramChat(s string) bool {
	n, err := strconv.ParseInt(s, 10, 64)
	return err == nil && n != 0 && n >= -9007199254740991 && n <= 9007199254740991 && strconv.FormatInt(n, 10) == s
}
func validWAChat(s string) bool {
	if identity.IsPerson(s) || s == "status@broadcast" {
		return true
	}
	if !strings.HasSuffix(s, "@g.us") {
		return false
	}
	id := strings.TrimSuffix(s, "@g.us")
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}
func validateJSON(raw []byte, object bool) error {
	if object && (len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{') {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if uniqueJSONValue(d, 0) != nil {
		return ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	return nil
}
