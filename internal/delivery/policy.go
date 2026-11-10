package delivery

import (
	"errors"
	"time"
)

// Outcome is implemented by safe provider errors, without importing providers
// into the durable execution layer or retaining raw SDK descriptions.
type Outcome interface {
	DeliveryOutcome() (retryable, uncertain bool, delay time.Duration)
}

type localRetry struct{}

func (localRetry) Error() string                                { return "local operation will retry" }
func (localRetry) DeliveryOutcome() (bool, bool, time.Duration) { return true, false, 0 }

// ErrLocalRetry is for a failed local read/write when all external effects have
// confirmed outcomes. It must not replace an ambiguous provider error.
var ErrLocalRetry error = localRetry{}

func decision(err error) (retry, uncertain bool, delay time.Duration) {
	if err == nil {
		return false, false, 0
	}
	if errors.Is(err, ErrUncertain) || errors.Is(err, ErrInFlight) {
		return false, true, 0
	}
	var classified Outcome
	if errors.As(err, &classified) {
		return classified.DeliveryOutcome()
	}
	if errors.Is(err, ErrInvalid) || errors.Is(err, ErrUnsupported) || errors.Is(err, ErrIncomplete) || errors.Is(err, ErrDiscarded) {
		return false, false, 0
	}
	// Internal failures might occur after a remote mutation, including a panic.
	return false, true, 0
}
