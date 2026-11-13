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
	"mime"
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
	Operation  string
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

func (e *Error) DeliveryOutcome() (bool, bool, time.Duration) {
	return e.Retry, e.Uncertain, e.RetryAfter
}

type attemptKey struct{}

// Attempt belongs to one synchronous SDK call; it is never shared between calls.
type Attempt struct {
	operation                string
	started                  bool
	streaming                bool
	status                   int
	code                     string
	reason                   string
	retryAfter               time.Duration
	declined                 bool
	networkError             error
	mediaLimit               int64
	mediaExceeded            bool
	mediaFilename, mediaMIME string
}

var ErrMediaLimit = errors.New("provider attachment exceeds configured limit")

// ObserveMedia bounds raw SDK media reads, including stored-media APIs whose
// native SDK surface returns a byte slice rather than a streaming reader.
func ObserveMedia(ctx context.Context, max int64) (context.Context, *Attempt) {
	ctx, a := ObserveStream(ctx)
	a.mediaLimit = max
	return ctx, a
}
func (a *Attempt) MediaMetadata() (string, string, bool) {
	return a.mediaFilename, a.mediaMIME, a.mediaExceeded
}

type cappedMediaBody struct {
	io.ReadCloser
	attempt   *Attempt
	remaining int64
}

func (b *cappedMediaBody) Read(p []byte) (int, error) {
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	if b.remaining < 0 {
		b.attempt.mediaExceeded = true
		b.ReadCloser.Close()
		return 0, ErrMediaLimit
	}
	return n, err
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
		for _, operation := range []string{"sendMessage", "getChat", "getChatMember", "logout", "logOut"} {
			if strings.HasSuffix(request.URL.Path, "/"+operation) {
				a.operation = operation
			}
		}
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
		if _, params, err := mime.ParseMediaType(response.Header.Get("Content-Disposition")); err == nil {
			a.mediaFilename = params["filename"]
		}
		a.mediaMIME = strings.TrimSpace(strings.Split(response.Header.Get("X-Media-Mimetype"), ";")[0])
		if a.mediaMIME == "" {
			a.mediaMIME = strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0])
		}
		if a.mediaLimit > 0 {
			if response.ContentLength > a.mediaLimit {
				a.mediaExceeded = true
				response.Body.Close()
				return nil, ErrMediaLimit
			}
			response.Body = &cappedMediaBody{ReadCloser: response.Body, attempt: a, remaining: a.mediaLimit}
		}
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
		case strings.Contains(description, "not modified") || strings.Contains(description, "not_modified"):
			a.reason = "not_modified"
		case strings.Contains(description, "message thread not found"):
			a.reason = "thread_missing"
		case (strings.Contains(description, "reply") || strings.Contains(description, "replied")) && strings.Contains(description, "not found"):
			a.reason = "reply_missing"
		case strings.Contains(description, "message") && strings.Contains(description, "not found"):
			a.reason = "message_missing"
		case strings.Contains(description, "participant_id_invalid"):
			a.reason = "participant_invalid"
		case strings.Contains(description, "user not found") || strings.Contains(description, "can't find user") || strings.Contains(description, "user_id_invalid"):
			a.reason = "user_missing"
		case strings.Contains(description, "can't parse entities") || strings.Contains(description, "entity_bounds_invalid"):
			a.reason = "entities_invalid"
		}
		if a.reason == "" && strings.HasPrefix(envelope.Description, "Bad Request: ") {
			// Telegram also returns bare MTProto error names. Retain only an
			// entire uppercase code, never arbitrary descriptions or arguments.
			code := strings.TrimPrefix(envelope.Description, "Bad Request: ")
			valid := code != "" && len(code) <= 64
			for _, r := range code {
				if r != '_' && (r < 'A' || r > 'Z') {
					valid = false
					break
				}
			}
			if valid {
				a.reason = strings.ToLower(code)
			}
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
	e := &Error{Service: service, Operation: a.operation, Status: a.status, Reason: a.reason, Code: "REFUSED", RetryAfter: a.retryAfter}
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
	e.Retry = !mutation && a.status >= 500 && a.status != 501
	return e
}

func InvalidResponse(service string, mutation bool) error {
	return &Error{Service: service, Status: 200, Code: "INVALID_RESPONSE", Uncertain: mutation, Retry: !mutation}
}
