// Package providers classifies delivery outcomes without exposing provider secrets or bodies.
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var ErrInvalidRequest = errors.New("invalid provider request")

// Error intentionally does not unwrap the SDK error: SDK errors can contain tokens and message bodies.
type Error struct {
	Service    string
	Status     int
	Code       string
	Reason     string
	Retry      bool
	Uncertain  bool
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s request failed (status=%d code=%s)", e.Service, e.Status, e.Code)
}

type attemptKey struct{}

// Attempt belongs to one synchronous SDK call; it is never shared between calls.
type Attempt struct {
	started      bool
	streaming    bool
	status       int
	code         string
	reason       string
	retryAfter   time.Duration
	declined     bool
	networkError error
}

func Observe(ctx context.Context) (context.Context, *Attempt) {
	a := &Attempt{}
	return context.WithValue(ctx, attemptKey{}, a), a
}

// ObserveStream bypasses JSON buffering only for successful GET media responses.
func ObserveStream(ctx context.Context) (context.Context, *Attempt) {
	ctx, attempt := Observe(ctx)
	attempt.streaming = true
	return ctx, attempt
}

func ValidURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Hostname() != "" && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func ValidKey(key string) bool {
	if len(key) < 1 || len(key) > 255 {
		return false
	}
	for _, c := range []byte(key) {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

type transport struct{ base http.RoundTripper }

// HTTPClient never follows redirects or enables SDK retry middleware.
func HTTPClient(base http.RoundTripper) *http.Client {
	if base == nil {
		t := http.DefaultTransport.(*http.Transport).Clone()
		// Avoid opaque retries on reused connections. Each mutation must have an observable outcome.
		t.DisableKeepAlives = true
		t.Protocols = new(http.Protocols)
		t.Protocols.SetHTTP1(true)
		base = t
	}
	return &http.Client{Transport: &transport{base: base}, Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (t *transport) RoundTrip(request *http.Request) (*http.Response, error) {
	a, _ := request.Context().Value(attemptKey{}).(*Attempt)
	if a != nil {
		a.started = true
	}
	response, err := t.base.RoundTrip(request)
	if err != nil {
		if a != nil {
			a.networkError = err
		}
		return nil, err
	}
	if response == nil {
		return nil, errors.New("provider returned no response")
	}
	if response.Body == nil {
		response.Body = http.NoBody
	}
	if a != nil {
		a.status = response.StatusCode
	}
	if a != nil && a.streaming && request.Method == http.MethodGet && response.StatusCode >= 200 && response.StatusCode < 300 {
		return response, nil // The SDK enforces the explicit byte cap and stream lifetime.
	}
	// Text/control JSON only; source streams use the bounded SDK reader above.
	const maxResponse = 8 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	_ = response.Body.Close()
	if err != nil || len(body) > maxResponse {
		return nil, errors.New("provider response unreadable or too large")
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	if a == nil {
		return response, nil
	}
	var envelope struct {
		OK                *bool   `json:"ok"`
		ErrorCode         int     `json:"error_code"`
		Code              string  `json:"code"`
		Description       string  `json:"description"`
		RetryAfterSeconds float64 `json:"retryAfterSeconds"`
		Parameters        struct {
			RetryAfter float64 `json:"retry_after"`
		} `json:"parameters"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		a.code = envelope.Code
		if envelope.OK != nil && !*envelope.OK {
			a.declined = true
			if envelope.ErrorCode >= 400 && envelope.ErrorCode <= 599 {
				a.status = envelope.ErrorCode
			}
		}
		description := strings.ToLower(envelope.Description)
		switch {
		case strings.Contains(description, "message thread not found"):
			a.reason = "thread_missing"
		case (strings.Contains(description, "reply") || strings.Contains(description, "replied")) && strings.Contains(description, "not found"):
			a.reason = "reply_missing"
		case strings.Contains(description, "message") && strings.Contains(description, "not found"):
			a.reason = "message_missing"
		}
		seconds := envelope.RetryAfterSeconds
		if seconds <= 0 {
			seconds = envelope.Parameters.RetryAfter
		}
		if seconds > 0 {
			if seconds >= float64(math.MaxInt64)/float64(time.Second) {
				a.retryAfter = time.Duration(math.MaxInt64)
			} else {
				a.retryAfter = time.Duration(seconds * float64(time.Second))
			}
		}
	}
	if a.retryAfter == 0 {
		if n, err := strconv.ParseInt(response.Header.Get("Retry-After"), 10, 32); err == nil && n > 0 {
			a.retryAfter = time.Duration(n) * time.Second
		} else if date, err := http.ParseTime(response.Header.Get("Retry-After")); err == nil && date.After(time.Now()) {
			a.retryAfter = time.Until(date)
		}
	}
	return response, nil
}

// Outcome maps one SDK result to the durable delivery policy. No SDK error text escapes.
func Outcome(service string, mutation bool, err error, a *Attempt) error {
	if err == nil && a.status >= 200 && a.status < 300 && !a.declined {
		return nil
	}
	e := &Error{Service: service, Status: a.status, Reason: a.reason, Code: "REFUSED", RetryAfter: a.retryAfter}
	if !a.started {
		e.Code = "INVALID_REQUEST"
		return e
	}
	switch a.code {
	case "IDEMPOTENCY_OUTCOME_UNKNOWN":
		e.Code = a.code
		e.Uncertain = true
		return e
	case "IDEMPOTENCY_KEY_IN_PROGRESS":
		e.Code = a.code
		e.Retry = true
		if e.RetryAfter == 0 {
			e.RetryAfter = 5 * time.Second
		}
		return e
	case "SEND_PACING_LIMITED", "IDEMPOTENCY_KEY_REUSED", "IDEMPOTENCY_KEY_INVALID", "MEDIA_NOT_STORED", "MEDIA_SOURCE_UNAVAILABLE":
		e.Code = a.code
	}
	if a.status == 429 {
		e.Retry = true
		if e.RetryAfter == 0 {
			e.RetryAfter = 5 * time.Second
		}
		return e
	}
	if a.networkError != nil {
		var dns *net.DNSError
		if errors.As(a.networkError, &dns) || errors.Is(a.networkError, syscall.ECONNREFUSED) {
			e.Code = "UNREACHABLE"
			e.Retry = true
			return e
		}
	}
	if a.status == 0 || (a.status >= 200 && a.status < 400) || (a.status >= 500 && a.status != 501) || (service == "openwa" && a.status == 409) {
		e.Code = "OUTCOME_UNKNOWN"
		e.Uncertain = mutation
		e.Retry = !mutation
		return e
	}
	e.Retry = !mutation && a.status >= 500
	return e
}

func InvalidResponse(service string, mutation bool) error {
	return &Error{Service: service, Status: 200, Code: "INVALID_RESPONSE", Uncertain: mutation, Retry: !mutation}
}
