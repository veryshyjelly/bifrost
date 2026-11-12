package legacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"

	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/identity"
)

var ErrTopicEvidence = errors.New("legacy topic reservation lacks exact retained topic evidence")

type TopicCoordinate struct{ Chat, Thread int64 }
type ImportedInbox struct {
	ChatID string
	Target TopicCoordinate
}
type ImportedRetiredTopic struct {
	ChatID string
	Target TopicCoordinate
}
type ImportedRoute struct {
	Route     conversation.Route
	ReturnKey string
	TopicName string
	// A source crash may leave the old route pointing at its own confirmed
	// retired topic. Keep that evidence fenced until retirement resumes.
	PendingRetirement bool
}
type RouteProjection struct {
	Routes  []ImportedRoute
	Inboxes []ImportedInbox
	Retired []ImportedRetiredTopic
}

func legacyTopicKey(target TopicCoordinate) string {
	return legacyTopicStringKey(strconv.FormatInt(target.Chat, 10), target.Thread)
}

func legacyTopicStringKey(chat string, thread int64) string {
	raw, _ := json.Marshal([]any{chat, thread})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func validTopic(t TopicCoordinate) bool {
	return t.Chat < 0 && t.Chat >= -9007199254740991 && t.Thread > 1 && t.Thread <= math.MaxInt32
}

// ProjectRoutes converts known destinations without creating topics or
// resurrecting retired ones. Candidate coordinates must come from retained
// source receipts/observations; the exact legacy hash identifies reservations.
// A missing match is a blocking review condition, never a dropped reservation.
func ProjectRoutes(c Configuration, values []KV, directory *identity.Directory, evidence []TopicCoordinate) (RouteProjection, error) {
	var result RouteProjection
	if c.management >= 0 || directory == nil {
		return result, ErrInvalid
	}
	coordinates := map[string]TopicCoordinate{}
	remember := func(target TopicCoordinate) {
		coordinates[legacyTopicKey(target)] = target
		if target.Chat == c.management {
			coordinates[legacyTopicStringKey(c.Value("TELEGRAM_MANAGEMENT_CHAT_ID"), target.Thread)] = target
		}
	}
	for _, target := range evidence {
		if !validTopic(target) {
			return result, ErrInvalid
		}
		remember(target)
	}
	seen := map[string]bool{}
	routeIDs := map[string]bool{}
	targets := map[TopicCoordinate]string{}
	inboxes := map[ImportedInbox]bool{}
	ordered := append([]KV(nil), values...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Kind != ordered[j].Kind {
			return ordered[i].Kind < ordered[j].Kind
		}
		return ordered[i].ID < ordered[j].ID
	})
	for _, v := range ordered {
		if v.Kind != "route" {
			continue
		}
		if seen[v.ID] || validateJSON([]byte(v.Value), true) != nil {
			return result, ErrInvalid
		}
		seen[v.ID] = true
		var source struct {
			ChatID       string `json:"chatId"`
			Name         string
			TGChat       json.RawMessage `json:"tgChat"`
			Thread       int64
			Mode         conversation.Mode
			InboxRetired bool `json:"inboxRetired"`
			Inbox        *struct {
				Chat   json.RawMessage
				Thread int64
			}
			ReturnKey string  `json:"inboxReturnKey"`
			TopicName *string `json:"topicSyncedName"`
		}
		if json.Unmarshal([]byte(v.Value), &source) != nil || !validWAChat(v.ID) || v.ID == "status@broadcast" || source.ChatID != v.ID {
			return result, ErrInvalid
		}
		chat := directory.Canonical(v.ID)
		if routeIDs[chat] {
			return result, ErrInvalid
		}
		routeIDs[chat] = true
		tg, err := legacyTelegramID(source.TGChat)
		if err != nil || tg >= 0 || source.Thread < 0 || source.Thread == 1 || source.Thread > math.MaxInt32 {
			return result, ErrInvalid
		}
		if source.Mode == "" {
			source.Mode = conversation.Inbox
		}
		switch source.Mode {
		case conversation.Inbox, conversation.Dedicated, conversation.Group, conversation.Muted, conversation.Ignored:
		default:
			return result, ErrInvalid
		}
		r := ImportedRoute{Route: conversation.Route{ChatID: chat, Name: source.Name, TelegramChatID: tg, ThreadID: source.Thread, Mode: source.Mode, InboxRetired: source.InboxRetired}, ReturnKey: source.ReturnKey, TopicName: source.Name}
		if source.TopicName != nil {
			r.TopicName = *source.TopicName
		}
		target := TopicCoordinate{Chat: tg, Thread: source.Thread}
		// Retired private inboxes use management/0 as an inactive placeholder.
		if target.Thread > 1 || target.Chat != c.management {
			if old, ok := targets[target]; ok && old != chat {
				return result, ErrInvalid
			}
			targets[target] = chat
		}
		if validTopic(target) {
			remember(target)
		}
		if target.Chat == c.management && validTopic(target) {
			inboxes[ImportedInbox{ChatID: chat, Target: target}] = true
		}
		if source.Inbox != nil {
			inboxChat, err := legacyTelegramID(source.Inbox.Chat)
			target := TopicCoordinate{Chat: inboxChat, Thread: source.Inbox.Thread}
			if err != nil || !validTopic(target) {
				return result, ErrInvalid
			}
			remember(target)
			inboxes[ImportedInbox{ChatID: chat, Target: target}] = true
		}
		result.Routes = append(result.Routes, r)
	}
	reserved := map[TopicCoordinate]string{}
	for _, v := range ordered {
		if v.Kind != "retired-topic" {
			continue
		}
		var source struct {
			ChatID string `json:"chatId"`
		}
		if validateJSON([]byte(v.Value), true) != nil || json.Unmarshal([]byte(v.Value), &source) != nil || !validWAChat(source.ChatID) || source.ChatID == "status@broadcast" {
			return result, ErrInvalid
		}
		target, ok := coordinates[v.ID]
		if !ok {
			return result, ErrTopicEvidence
		}
		chat := directory.Canonical(source.ChatID)
		if old, ok := reserved[target]; ok && old != chat {
			return result, ErrInvalid
		}
		reserved[target] = chat
		result.Retired = append(result.Retired, ImportedRetiredTopic{ChatID: chat, Target: target})
		inboxes[ImportedInbox{ChatID: chat, Target: target}] = true
	}
	for i := range result.Routes {
		r := &result.Routes[i]
		target := TopicCoordinate{Chat: r.Route.TelegramChatID, Thread: r.Route.ThreadID}
		if retired, ok := reserved[target]; ok {
			if retired != r.Route.ChatID {
				return result, ErrInvalid
			}
			r.PendingRetirement = true
		}
	}
	inboxOwners := map[TopicCoordinate]string{}
	for inbox := range inboxes {
		if owner, ok := inboxOwners[inbox.Target]; ok && owner != inbox.ChatID {
			return result, ErrInvalid
		}
		inboxOwners[inbox.Target] = inbox.ChatID
		if retired, ok := reserved[inbox.Target]; ok && retired != inbox.ChatID {
			return result, ErrInvalid
		}
		if owner, ok := targets[inbox.Target]; ok && owner != inbox.ChatID {
			return result, ErrInvalid
		}
		result.Inboxes = append(result.Inboxes, inbox)
	}
	sort.Slice(result.Inboxes, func(i, j int) bool {
		a, b := result.Inboxes[i], result.Inboxes[j]
		if a.ChatID != b.ChatID {
			return a.ChatID < b.ChatID
		}
		if a.Target.Chat != b.Target.Chat {
			return a.Target.Chat < b.Target.Chat
		}
		return a.Target.Thread < b.Target.Thread
	})
	return result, nil
}

func legacyTelegramID(raw json.RawMessage) (int64, error) {
	var value string
	if len(raw) == 0 {
		return 0, ErrInvalid
	}
	if raw[0] == '"' {
		if json.Unmarshal(raw, &value) != nil {
			return 0, ErrInvalid
		}
	} else {
		value = string(raw)
	}
	if !validTelegramChat(value) {
		return 0, ErrInvalid
	}
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err
}
