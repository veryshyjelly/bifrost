package identity

import (
	"encoding/json"
	"strings"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

// Source is a durable snapshot of message identity and content, taken before
// routing or sending. ChatID is the stable conversation key; RawChatID retains
// the provider address that supplied this message.
type Source struct {
	ChatID, RawChatID, MessageID, SenderID, LinkSenderID string
	Name, ConversationName, Body, QuotedMessageID        string
	Own                                                  bool
	MentionedIDs                                         []string
}

type MessageEvent struct {
	Event, SessionID, AccountID, IdempotencyKey string
	Message                                     sdk.ChatHistoryMessage
}

// DecodeSource validates a message envelope against its durable job account and dedup key.
// Status, channel and broadcast events require separate handlers.
func DecodeSource(job delivery.Job) (MessageEvent, error) {
	var envelope struct {
		Event          string          `json:"event"`
		SessionID      string          `json:"sessionId"`
		AccountID      string          `json:"accountId"`
		IdempotencyKey string          `json:"idempotencyKey"`
		Data           json.RawMessage `json:"data"`
	}
	if job.Kind != "wa" || json.Unmarshal(job.Payload, &envelope) != nil {
		return MessageEvent{}, delivery.ErrInvalid
	}
	if envelope.Event != sdk.EventMessageReceived && envelope.Event != sdk.EventMessageSent {
		return MessageEvent{}, delivery.ErrUnsupported
	}
	if envelope.AccountID != job.AccountID || envelope.SessionID == "" || envelope.IdempotencyKey == "" || job.Dedup != "ow:"+envelope.IdempotencyKey {
		return MessageEvent{}, delivery.ErrInvalid
	}
	var m sdk.ChatHistoryMessage
	if json.Unmarshal(envelope.Data, &m) != nil || m.ID == "" || m.ChatID == "" {
		return MessageEvent{}, delivery.ErrInvalid
	}
	if m.IsStatusBroadcast || m.Kind == sdk.ChatKind("channel") || m.Kind == sdk.ChatKind("status") || m.Kind == sdk.ChatKind("broadcast") {
		return MessageEvent{}, delivery.ErrUnsupported
	}
	if !IsPerson(m.ChatID) && !strings.HasSuffix(m.ChatID, "@g.us") {
		return MessageEvent{}, delivery.ErrInvalid
	}
	return MessageEvent{Event: envelope.Event, SessionID: envelope.SessionID, AccountID: envelope.AccountID, IdempotencyKey: envelope.IdempotencyKey, Message: m}, nil
}

// SenderName retains the original full sender label. Mention labels have their
// separate 48-codepoint sanitizer; sender headers do not acquire that limit.
func (d *Directory) SenderName(id string) string {
	id = d.Canonical(id)
	for _, preferred := range []string{d.boundProfiles[id], id} {
		for _, p := range d.profiles {
			if preferred != "" && p.ID == preferred && p.Name != "" {
				return p.Name
			}
		}
	}
	for _, p := range d.profiles {
		if d.Canonical(p.ID) == id && p.Name != "" {
			return p.Name
		}
	}
	return id
}
