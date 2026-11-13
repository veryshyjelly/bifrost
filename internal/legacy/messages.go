package legacy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/presentation"
)

var ErrMappingEvidence = errors.New("legacy message mapping lacks consistent scoped destination evidence")

type MessageEvidence struct {
	Mappings     []Message
	Observations []TelegramMessage
	Values       []KV
	Jobs         []Job
	Effects      []Effect
}
type ImportedMapping struct {
	Message       Message
	Thread        int64
	Receipt       json.RawMessage
	ReceiptOrigin string
	Presentation  presentation.Options
	MentionedIDs  []string
	SourcePayload json.RawMessage
}
type ImportedObservation struct {
	Observation TelegramMessage
	Payload     json.RawMessage
}
type MessageProjection struct {
	Mappings     []ImportedMapping
	Observations []ImportedObservation
	SelfID       string
}

type telegramKey struct {
	bot      string
	chat, id int64
}
type observationKey struct {
	scope    string
	chat, id int64
}
type receiptEvidence struct {
	raw             json.RawMessage
	message         models.Message
	origin          string
	requestedThread *int64
}
type targetEvidence struct {
	prefix, bot string
	chat        int64
	thread      *int64
}
type historyEvidence struct {
	historical bool
	timestamp  int64
	mentioned  json.RawMessage
}

func telegramReceiptKey(k telegramKey) string {
	raw, _ := json.Marshal([]any{k.bot, strconv.FormatInt(k.chat, 10), k.id})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func telegramScope(bot string, chat int64) string {
	if chat < 0 {
		return "*"
	}
	return bot
}
func normalizedThread(thread int64) int64 {
	if thread == 1 {
		return 0
	}
	return thread
}

func decodeReceipt(raw []byte) (receiptEvidence, bool) {
	var result receiptEvidence
	if validateJSON(raw, true) != nil || json.Unmarshal(raw, &result.message) != nil || result.message.ID <= 0 || result.message.Chat.ID == 0 {
		return result, false
	}
	result.raw = bytes.Clone(raw)
	return result, true
}

// ProjectMessages preserves actual receipts and user observations separately.
// Mapping-only evidence has a null provider receipt. Group copies without a
// consistent retained location block projection rather than borrowing the
// current route's thread. The source mapping order is retained.
func ProjectMessages(c Configuration, account string, directory *identity.Directory, input MessageEvidence) (_ MessageProjection, err error) {
	phase := "configuration"
	defer func() {
		if err != nil {
			err = fmt.Errorf("legacy message %s evidence: %w", phase, err)
		}
	}()
	var result MessageProjection
	if directory == nil || c.master <= 0 {
		return result, ErrInvalid
	}
	if _, err := c.account(account); err != nil || !validAccount(account) {
		return result, ErrInvalid
	}
	cache := map[string]KV{}
	targets := map[string]targetEvidence{}
	senderBots := map[int64][]string{c.master: {"master"}}
	phase = "frozen target"
	for _, v := range input.Values {
		if _, duplicate := cache[v.Kind+"\x00"+v.ID]; duplicate {
			return result, ErrInvalid
		}
		cache[v.Kind+"\x00"+v.ID] = v
		if v.Kind == "bot" {
			id, err := strconv.ParseInt(v.ID, 10, 64)
			if err != nil || id <= 0 {
				return result, ErrInvalid
			}
			senderBots[id] = append(senderBots[id], v.ID)
		}
		if v.Kind != "delivery-target" {
			continue
		}
		var target struct {
			Bot         string
			Destination json.RawMessage
			Thread      *int64
		}
		if validateJSON([]byte(v.Value), true) != nil || json.Unmarshal([]byte(v.Value), &target) != nil {
			return result, ErrInvalid
		}
		chat, err := legacyTelegramID(target.Destination)
		if err != nil || target.Bot == "" {
			return result, ErrInvalid
		}
		if target.Thread != nil && (*target.Thread < 0 || *target.Thread > 2147483647) {
			return result, ErrInvalid
		}
		prefix := v.ID + ":to:" + target.Bot + ":" + strconv.FormatInt(chat, 10) + ":"
		targets[prefix] = targetEvidence{prefix: prefix, bot: target.Bot, chat: chat, thread: target.Thread}
	}
	scope := sha256.Sum256([]byte(account))
	phase = "self identity"
	if v, exists := cache["reaction-self\x00"+hex.EncodeToString(scope[:])]; exists {
		var self struct{ Sender string }
		if validateJSON([]byte(v.Value), true) != nil || json.Unmarshal([]byte(v.Value), &self) != nil {
			return result, ErrInvalid
		}
		id, valid := identity.PhoneID(identity.Local(self.Sender))
		if !valid || identity.Normalize(self.Sender) != id {
			return result, ErrInvalid
		}
		result.SelfID = id
	}
	// A logical master can have historical credentials. A frozen target prefix
	// or exact cached receipt retains that scope without guessing its old ID.
	effectReceipts := map[telegramKey]receiptEvidence{}
	phase = "confirmed receipt"
	for _, effect := range input.Effects {
		if effect.State != "done" || effect.Result == nil {
			continue
		}
		receipt, ok := decodeReceipt([]byte(*effect.Result))
		if !ok {
			continue
		}
		bot := ""
		var thread *int64
		ambiguous := false
		for _, target := range matchingTargets(effect.ID, targets) {
			if target.chat != receipt.message.Chat.ID || !strings.HasPrefix(effect.ID, target.prefix) {
				continue
			}
			if bot != "" && bot != target.bot {
				ambiguous = true
				break
			}
			bot, thread = target.bot, target.thread
		}
		if ambiguous {
			continue
		}
		if bot == "" && receipt.message.From != nil && receipt.message.From.IsBot {
			if candidates := senderBots[receipt.message.From.ID]; len(candidates) == 1 {
				bot = candidates[0]
			}
		}
		if bot == "" || !validReceiptSender(receipt.message, bot) {
			continue
		}
		receipt.origin = "confirmed-effect"
		receipt.requestedThread = thread
		key := telegramKey{bot: bot, chat: receipt.message.Chat.ID, id: int64(receipt.message.ID)}
		if previous, ok := effectReceipts[key]; !ok || (previous.requestedThread == nil && thread != nil) {
			effectReceipts[key] = receipt
		}
	}
	jobs := append([]Job(nil), input.Jobs...)
	phase = "source snapshot"
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })
	userMessages := map[telegramKey]json.RawMessage{}
	userSources := map[telegramKey]json.RawMessage{}
	historical := map[[2]string]historyEvidence{}
	for _, job := range jobs {
		if job.Type == "wa" {
			var event struct {
				Event      string
				Historical bool
				Data       struct {
					ID, ChatID   string
					Timestamp    int64
					MentionedIDs json.RawMessage `json:"mentionedIds"`
				}
			}
			if json.Unmarshal([]byte(job.Payload), &event) == nil && (event.Event == "message.received" || event.Event == "message.sent") && event.Data.ID != "" && validWAChat(event.Data.ChatID) {
				key := [2]string{directory.Canonical(event.Data.ChatID), event.Data.ID}
				if _, exists := historical[key]; !exists {
					historical[key] = historyEvidence{historical: event.Historical, timestamp: event.Data.Timestamp, mentioned: event.Data.MentionedIDs}
				}
			}
			continue
		}
		if job.Type != "tg" {
			continue
		}
		var source struct {
			Bot    string
			Update struct{ Message json.RawMessage }
		}
		if json.Unmarshal([]byte(job.Payload), &source) != nil || source.Bot == "" || len(source.Update.Message) == 0 {
			continue
		}
		message, ok := decodeReceipt(source.Update.Message)
		if !ok {
			continue
		}
		key := telegramKey{bot: source.Bot, chat: message.message.Chat.ID, id: int64(message.message.ID)}
		if _, exists := userMessages[key]; !exists {
			userMessages[key] = message.raw
			userSources[key] = json.RawMessage(job.Payload)
		}
	}
	observed := map[observationKey]TelegramMessage{}
	phase = "observation"
	for _, o := range input.Observations {
		chat, err := strconv.ParseInt(o.Chat, 10, 64)
		if err != nil {
			return result, ErrInvalid
		}
		expected := telegramScope(o.Bot, chat)
		if o.Scope != expected || o.Thread < 0 || o.Thread > 2147483647 {
			return result, ErrInvalid
		}
		switch o.Kind {
		case "chat", "command", "service", "protected", "log":
		default:
			return result, ErrInvalid
		}
		key := observationKey{scope: o.Scope, chat: chat, id: o.Message}
		if _, exists := observed[key]; exists {
			return result, ErrInvalid
		}
		observed[key] = o
		payload := json.RawMessage("null")
		if raw, exists := userMessages[telegramKey{bot: o.Bot, chat: chat, id: o.Message}]; exists {
			payload = raw
		} else if receipt, exists, err := cachedReceipt(cache, telegramKey{bot: o.Bot, chat: chat, id: o.Message}); err != nil {
			return result, err
		} else if exists {
			payload = receipt.raw
		}
		result.Observations = append(result.Observations, ImportedObservation{Observation: o, Payload: payload})
	}
	for _, mapping := range input.Mappings {
		phase = "mapping receipt"
		chat, err := strconv.ParseInt(mapping.TGChat, 10, 64)
		if err != nil {
			return result, ErrInvalid
		}
		key := telegramKey{bot: mapping.Bot, chat: chat, id: mapping.TGID}
		receipt, hasReceipt, err := cachedReceipt(cache, key)
		if err != nil {
			return result, err
		}
		if !hasReceipt {
			receipt, hasReceipt = effectReceipts[key]
		}
		phase = "mapping location"
		thread, err := mappingThread(chat, observed[observationKey{scope: telegramScope(mapping.Bot, chat), chat: chat, id: mapping.TGID}], receipt, hasReceipt, userMessages[key])
		if err != nil {
			return result, err
		}
		phase = "mapping presentation"
		format, mentioned, err := mappingPresentation(result.SelfID, directory, mapping, cache, historical)
		if err != nil {
			return result, err
		}
		row := ImportedMapping{Message: mapping, Thread: thread, Receipt: json.RawMessage("null"), ReceiptOrigin: "mapping-only", Presentation: format, MentionedIDs: mentioned, SourcePayload: userSources[key]}
		row.Message.WAChat = directory.Canonical(mapping.WAChat)
		if hasReceipt {
			row.Receipt, row.ReceiptOrigin = receipt.raw, receipt.origin
		}
		result.Mappings = append(result.Mappings, row)
	}
	return result, nil
}

func validReceiptSender(message models.Message, bot string) bool {
	if message.From == nil {
		return true
	}
	if !message.From.IsBot {
		return false
	}
	return bot == "master" || strconv.FormatInt(message.From.ID, 10) == bot
}

func cachedReceipt(cache map[string]KV, key telegramKey) (receiptEvidence, bool, error) {
	v, found := cache["telegram-receipt\x00"+telegramReceiptKey(key)]
	if !found {
		return receiptEvidence{}, false, nil
	}
	receipt, ok := decodeReceipt([]byte(v.Value))
	if !ok || int64(receipt.message.ID) != key.id || receipt.message.Chat.ID != key.chat || !validReceiptSender(receipt.message, key.bot) {
		return receiptEvidence{}, false, ErrMappingEvidence
	}
	receipt.origin = "cached-receipt"
	return receipt, true, nil
}

func mappingThread(chat int64, observation TelegramMessage, receipt receiptEvidence, hasReceipt bool, userRaw json.RawMessage) (int64, error) {
	known := chat > 0
	thread := int64(0)
	if observation.Seq > 0 {
		known = true
		thread = normalizedThread(observation.Thread)
	}
	check := func(candidate int64, explicit bool) error {
		candidate = normalizedThread(candidate)
		if candidate < 0 || candidate > 2147483647 {
			return ErrMappingEvidence
		}
		if known && explicit && candidate != thread {
			return ErrMappingEvidence
		}
		if !known {
			thread = candidate
			known = true
		}
		return nil
	}
	if hasReceipt {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(receipt.raw, &fields)
		if receipt.requestedThread != nil {
			if err := check(*receipt.requestedThread, true); err != nil {
				return 0, err
			}
		}
		_, explicit := fields["message_thread_id"]
		if explicit || !known {
			// Telegram can return an incidental reply-thread ID in an
			// ordinary supergroup. As native delivery does, retain a proven
			// main-chat destination unless this is a real topic message.
			if known && thread == 0 && !receipt.message.IsTopicMessage {
				explicit = false
			}
			if err := check(int64(receipt.message.MessageThreadID), explicit); err != nil {
				return 0, err
			}
		}
	}
	if len(userRaw) > 0 {
		var message models.Message
		if json.Unmarshal(userRaw, &message) != nil {
			return 0, ErrMappingEvidence
		}
		if err := check(int64(message.MessageThreadID), !(known && thread == 0 && !message.IsTopicMessage)); err != nil {
			return 0, err
		}
	}
	if !known {
		return 0, ErrMappingEvidence
	}
	return thread, nil
}

func mappingPresentation(selfID string, directory *identity.Directory, mapping Message, cache map[string]KV, historical map[[2]string]historyEvidence) (presentation.Options, []string, error) {
	var source struct {
		FromMe                   bool `json:"fromMe"`
		LinkSender, Sender, Name string
		Historical               *bool
		Status                   bool
		Timestamp                int64
		MentionedIDs             json.RawMessage `json:"mentionedIds"`
	}
	for _, chat := range []string{mapping.WAChat, directory.Canonical(mapping.WAChat)} {
		if v, exists := cache["wamessage\x00"+chat+":"+mapping.WAID]; exists {
			if validateJSON([]byte(v.Value), true) != nil || json.Unmarshal([]byte(v.Value), &source) != nil {
				return presentation.Options{}, nil, ErrInvalid
			}
			break
		}
	}
	if source.Historical == nil {
		if event, exists := historical[[2]string{directory.Canonical(mapping.WAChat), mapping.WAID}]; exists {
			source.Historical = &event.historical
			source.Timestamp = event.timestamp
			source.MentionedIDs = event.mentioned
		}
	}
	mentioned, err := legacyMentionedIDs(source.MentionedIDs)
	if err != nil {
		return presentation.Options{}, nil, err
	}
	chat, _ := strconv.ParseInt(mapping.TGChat, 10, 64)
	if v, exists := cache["message-presentation\x00"+telegramReceiptKey(telegramKey{bot: mapping.Bot, chat: chat, id: mapping.TGID})]; exists {
		var options presentation.Options
		if validateJSON([]byte(v.Value), true) != nil || json.Unmarshal([]byte(v.Value), &options) != nil {
			return options, nil, ErrInvalid
		}
		options.Bot = mapping.Bot
		return options, mentioned, nil
	}
	options := presentation.Options{Bot: mapping.Bot, Name: source.Name, Own: source.FromMe || mapping.Direction == "out", Status: source.Status, Timestamp: source.Timestamp}
	if source.Historical != nil {
		options.Historical = *source.Historical
	}
	if options.Name == "" {
		options.Name = "WhatsApp"
	}
	link := source.LinkSender
	if link == "" {
		link = source.Sender
	}
	if link == "" {
		link = directory.Canonical(mapping.WAChat)
	}
	if options.Own {
		link = selfID
	}
	options.Sender = directory.Canonical(link)
	return options, mentioned, nil
}

func matchingTargets(key string, targets map[string]targetEvidence) []targetEvidence {
	var result []targetEvidence
	for offset := 0; offset < len(key); {
		i := strings.Index(key[offset:], ":to:")
		if i < 0 {
			break
		}
		i += offset
		offset = i + 4
		botEnd := strings.IndexByte(key[offset:], ':')
		if botEnd < 0 {
			continue
		}
		botEnd += offset
		chatEnd := strings.IndexByte(key[botEnd+1:], ':')
		if chatEnd < 0 {
			continue
		}
		chatEnd += botEnd + 1
		if target, found := targets[key[:chatEnd+1]]; found {
			result = append(result, target)
		}
	}
	return result
}

func legacyMentionedIDs(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return nil, ErrInvalid
	}
	ids := make([]string, 0, len(values))
	for _, raw := range values {
		var id string
		if json.Unmarshal(raw, &id) != nil {
			var wid struct {
				Serialized string `json:"_serialized"`
				Value      string `json:"$1"`
			}
			if json.Unmarshal(raw, &wid) != nil {
				continue
			}
			id = wid.Serialized
			if id == "" {
				id = wid.Value
			}
		}
		if identity.IsPerson(id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
