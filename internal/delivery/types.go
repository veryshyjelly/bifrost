package delivery

import (
	"encoding/json"
	"errors"
)

var (
	ErrNoJob      = errors.New("no eligible job")
	ErrClaimLost  = errors.New("job claim expired or belongs to another worker")
	ErrUncertain  = errors.New("delivery outcome requires explicit review")
	ErrInFlight   = errors.New("effect is already sending")
	ErrIncomplete = errors.New("job has incomplete effects")
	ErrInvalid    = errors.New("invalid delivery operation")
)

type State string

const (
	Pending   State = "pending"
	Failed    State = "failed"
	Uncertain State = "uncertain"
	Done      State = "done"
)

// Event is the durable input, scoped to a permanent account and ordered stream.
type Event struct {
	AccountID string
	Dedup     string
	Stream    string
	Kind      string
	Payload   json.RawMessage
}

// Job identifies one fenced worker claim. Tokens must never be logged.
type Job struct {
	ID int64
	Event
	Token    string
	Attempts int32
}

// Effect carries the frozen request or an immutable successful receipt.
type Effect struct {
	State   string
	Request json.RawMessage
	Result  json.RawMessage
}
