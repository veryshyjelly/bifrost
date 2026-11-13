package legacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"

	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/statuses"
)

type StatusSkip struct {
	SourceKey, Author string
	At                int64
}
type StatusMuteOperation struct {
	SourceKey, JobKey string
	Mute              statuses.Mute
}
type StatusState struct {
	Feed       *statuses.Topic
	Mutes      []statuses.Mute
	Revision   int64
	Skipped    []StatusSkip
	Operations []StatusMuteOperation
}

func StatusScope(account string) string {
	sum := sha256.Sum256([]byte(account))
	return hex.EncodeToString(sum[:])
}
func StatusSkipKey(account, id string) string { return stringTupleKey(StatusScope(account), id) }

// ProjectStatuses retains saved feed ownership, mute history and permanent
// skip hashes. It does not invent post bodies, source jobs or send receipts.
func ProjectStatuses(account string, c Configuration, values []KV, jobs []Job, directory *identity.Directory) (StatusState, error) {
	var result StatusState
	if !validAccount(account) || directory == nil || c.OwnerID() <= 0 || c.ManagementChatID() >= 0 {
		return result, ErrInvalid
	}
	scope := StatusScope(account)
	operations := map[string]string{}
	for _, job := range jobs {
		if job.Type != "tg" {
			continue
		}
		operations[stringTupleKey(scope, c.Value("TELEGRAM_OWNER_ID"), job.Dedup)] = job.Dedup
	}
	decodeMute := func(raw string) (statuses.Mute, error) {
		var saved struct {
			ID, Session, Sender, Name string
			Active                    *bool
			Revision, UpdatedAt       int64
		}
		if validateJSON([]byte(raw), true) != nil || json.Unmarshal([]byte(raw), &saved) != nil || saved.Session != scope || !identity.IsPerson(saved.Sender) || saved.Active == nil || saved.Revision <= 0 || saved.Revision > 9007199254740991 || saved.UpdatedAt < 0 || saved.UpdatedAt > 9007199254740991 {
			return statuses.Mute{}, ErrInvalid
		}
		valid := false
		for _, alias := range directory.Aliases(saved.Sender) {
			if saved.ID == stringTupleKey(scope, alias) {
				valid = true
			}
		}
		if !valid {
			return statuses.Mute{}, ErrInvalid
		}
		return statuses.Mute{Author: directory.Canonical(saved.Sender), Name: saved.Name, Active: *saved.Active, Revision: saved.Revision}, nil
	}
	ordered := append([]KV(nil), values...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Kind != ordered[j].Kind {
			return ordered[i].Kind < ordered[j].Kind
		}
		return ordered[i].ID < ordered[j].ID
	})
	seen := map[[2]string]bool{}
	mutes := map[string]statuses.Mute{}
	var maxRevision int64
	counter := false
	for _, value := range ordered {
		if value.Kind != "status-feed" && value.Kind != "status-mute" && value.Kind != "status-mute-counter" && value.Kind != "status-mute-op" && value.Kind != "status-skipped" {
			continue
		}
		key := [2]string{value.Kind, value.ID}
		if seen[key] || validateJSON([]byte(value.Value), false) != nil {
			return StatusState{}, ErrInvalid
		}
		seen[key] = true
		switch value.Kind {
		case "status-feed":
			var feed struct {
				ChatID, TGChat, Mode string
				Thread               int64
			}
			expected := stringTupleKey(account, c.Value("TELEGRAM_MANAGEMENT_CHAT_ID"))
			if result.Feed != nil || value.ID != expected || json.Unmarshal([]byte(value.Value), &feed) != nil || feed.ChatID != statuses.ChatID || feed.TGChat != c.Value("TELEGRAM_MANAGEMENT_CHAT_ID") || feed.Mode != "status" || feed.Thread <= 1 || feed.Thread > 2147483647 {
				return StatusState{}, ErrInvalid
			}
			result.Feed = &statuses.Topic{Feed: statuses.Feed{Management: c.ManagementChatID(), ThreadID: feed.Thread}, Name: "WhatsApp Status", Key: "status-topic:" + value.ID, Complete: true}
		case "status-mute":
			var id struct{ ID string }
			_ = json.Unmarshal([]byte(value.Value), &id)
			mute, err := decodeMute(value.Value)
			if err != nil || id.ID != value.ID {
				return StatusState{}, ErrInvalid
			}
			if previous, exists := mutes[mute.Author]; exists && previous.Revision == mute.Revision && !reflect.DeepEqual(previous, mute) {
				return StatusState{}, ErrInvalid
			}
			if previous, exists := mutes[mute.Author]; !exists || previous.Revision < mute.Revision {
				mutes[mute.Author] = mute
			}
			maxRevision = max(maxRevision, mute.Revision)
		case "status-mute-counter":
			var revision *int64
			if counter || value.ID != scope || json.Unmarshal([]byte(value.Value), &revision) != nil || revision == nil || *revision < 0 || *revision > 9007199254740991 {
				return StatusState{}, ErrInvalid
			}
			result.Revision = *revision
			counter = true
		case "status-mute-op":
			mute, err := decodeMute(value.Value)
			if err != nil || operations[value.ID] == "" {
				return StatusState{}, ErrInvalid
			}
			result.Operations = append(result.Operations, StatusMuteOperation{SourceKey: value.ID, JobKey: operations[value.ID], Mute: mute})
			maxRevision = max(maxRevision, mute.Revision)
		case "status-skipped":
			var skip struct {
				Sender string
				At     *int64
			}
			digest, err := hex.DecodeString(value.ID)
			if err != nil || len(digest) != 32 || hex.EncodeToString(digest) != value.ID || json.Unmarshal([]byte(value.Value), &skip) != nil || !identity.IsPerson(skip.Sender) || skip.At == nil || *skip.At < 0 || *skip.At > 9007199254740991 {
				return StatusState{}, ErrInvalid
			}
			result.Skipped = append(result.Skipped, StatusSkip{SourceKey: value.ID, Author: directory.Canonical(skip.Sender), At: *skip.At})
		}
	}
	if maxRevision > result.Revision || (maxRevision > 0 && !counter) {
		return StatusState{}, ErrInvalid
	}
	for _, mute := range mutes {
		result.Mutes = append(result.Mutes, mute)
	}
	sort.Slice(result.Mutes, func(i, j int) bool { return result.Mutes[i].Author < result.Mutes[j].Author })
	return result, nil
}
