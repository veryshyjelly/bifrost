package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

type EffectStore interface {
	BeginEffect(context.Context, Job, string, json.RawMessage) (Effect, error)
	ResolveEffect(context.Context, Job, string, State, json.RawMessage) error
	DiscardEffect(context.Context, Job, string) error
}

// Effects serializes a job's mutations and always invokes them with the frozen
// request. It deliberately has no general-purpose retry middleware.
type Effects struct {
	mu      sync.Mutex
	ctx     context.Context
	job     Job
	store   EffectStore
	fence   *lease
	timeout time.Duration
	stopped bool
	refused map[string]bool
}

func (e *Effects) persistedContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(e.ctx), e.timeout)
}

// Do returns an immutable receipt when complete. The callback must use its
// supplied context and must return a validated JSON receipt after remote success.
func (e *Effects) Do(key string, request json.RawMessage, action func(context.Context, json.RawMessage) (json.RawMessage, error)) (json.RawMessage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		return nil, ErrUncertain
	}
	if err := e.ctx.Err(); err != nil {
		return nil, err
	}
	if !e.fence.valid() {
		return nil, ErrClaimLost
	}
	if key == "" || !json.Valid(request) || action == nil {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(e.ctx, e.timeout)
	effect, err := e.store.BeginEffect(ctx, e.job, key, request)
	cancel()
	if err != nil {
		if errors.Is(err, ErrClaimLost) {
			e.fence.lose()
			return nil, ErrClaimLost
		}
		if errors.Is(err, ErrInvalid) || errors.Is(err, ErrInFlight) || errors.Is(err, ErrUncertain) {
			e.stopped = true
			return nil, err
		}
		e.stopped = true
		return nil, errors.Join(ErrPersistence, ErrUncertain)
	}
	switch effect.State {
	case "done":
		if !json.Valid(effect.Result) {
			e.stopped = true
			return nil, ErrUncertain
		}
		return append(json.RawMessage(nil), effect.Result...), nil
	case "discarded":
		return nil, ErrDiscarded
	case "sending":
	default:
		e.stopped = true
		return nil, ErrUncertain
	}
	if e.ctx.Err() != nil || !e.fence.valid() {
		// No external action was called, so retain the frozen request as pending
		// if ownership still permits a write. An expired claim is left for recovery.
		ctx, cancel := e.persistedContext()
		err := e.store.ResolveEffect(ctx, e.job, key, Pending, nil)
		cancel()
		if err != nil {
			e.stopped = true
			return nil, ErrUncertain
		}
		return nil, context.Canceled
	}
	result, callErr := invoke(e.ctx, effect.Request, action)
	state := Done
	if callErr != nil {
		_, unknown, _ := decision(callErr)
		state = Pending
		if unknown {
			state = Uncertain
			e.stopped = true
		}
	} else if len(result) == 0 {
		result = json.RawMessage("null")
	} else if !json.Valid(result) {
		state = Uncertain
		callErr = ErrUncertain
		e.stopped = true
	}
	if state != Done {
		result = nil
	}
	ctx, cancel = e.persistedContext()
	err = e.store.ResolveEffect(ctx, e.job, key, state, result)
	cancel()
	if err != nil {
		e.stopped = true
		if errors.Is(err, ErrClaimLost) {
			e.fence.lose()
			return nil, ErrClaimLost
		}
		return nil, errors.Join(ErrPersistence, ErrUncertain)
	}
	if callErr != nil && state == Pending {
		e.refused[key] = true
	}
	return result, callErr
}

func invoke(ctx context.Context, request json.RawMessage, action func(context.Context, json.RawMessage) (json.RawMessage, error)) (receipt json.RawMessage, err error) {
	defer func() {
		if recover() != nil {
			receipt = nil
			err = ErrUncertain
		}
	}()
	return action(ctx, append(json.RawMessage(nil), request...))
}

// Discard explicitly handles a definite refusal (for example, topic creation
// denied before an owner notice). It cannot convert an uncertain send to success.
func (e *Effects) Discard(key string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		return ErrUncertain
	}
	if !e.refused[key] {
		return ErrInvalid
	}
	if !e.fence.valid() {
		return ErrClaimLost
	}
	ctx, cancel := e.persistedContext()
	defer cancel()
	if err := e.store.DiscardEffect(ctx, e.job, key); err != nil {
		return ErrPersistence
	}
	delete(e.refused, key)
	return nil
}
