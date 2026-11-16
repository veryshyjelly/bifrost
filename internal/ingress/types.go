// Package ingress authenticates provider events and admits durable work without
// executing provider operations or acknowledging an uncommitted event.
// Provider-specific validation selects an account-scoped ordered stream; the
// delivery worker performs the resulting work after admission commits.
package ingress

import (
	"context"
	"encoding/json"
	"time"

	"github.com/veryshyjelly/bifrost/internal/delivery"
)

// Options binds ingress to one permanent account, provider session and owner.
// WebhookSecret and AdminToken authenticate separate ingress surfaces.
type Options struct {
	AccountID, SessionID, WebhookSecret, AdminToken string
	OwnerID, ManagementChatID, MasterBotID          int64
	Now                                             func() time.Time
}

type Bot struct{ ID, Username string }

// Route is the Telegram-facing portion of a conversation binding.
type Route struct {
	WhatsAppChat   string
	TelegramChatID int64
	ThreadID       int64
	InboxRetired   bool
}

// Observation retains cleanup eligibility without authorizing a delivery.
type Observation struct {
	AccountID, Scope, Bot, Kind string
	ChatID, MessageID, ThreadID int64
	Date                        int64
	Payload                     json.RawMessage
}

// Store is implemented by PostgreSQL. Admit commits observation and delivery
// together; a nil event records only an observation and returns accepted=false.
type Store interface {
	IngressBot(context.Context, string, string) (Bot, bool, error)
	IngressRoute(context.Context, string, int64, int64, int64) (Route, bool, error)
	Admit(context.Context, *delivery.Event, *Observation) (bool, error)
}
