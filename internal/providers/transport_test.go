package providers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestConnectionFailuresSeparatePreconnectionFromAmbiguousSends(t *testing.T) {
	for _, tc := range []struct {
		name             string
		cause            error
		retry, uncertain bool
	}{
		{"DNS", &net.DNSError{Name: "private-host", Err: "private-body"}, true, false},
		{"refused", syscall.ECONNREFUSED, true, false},
		{"reset", syscall.ECONNRESET, false, true},
		{"deadline", context.DeadlineExceeded, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, a := Observe(t.Context())
			request, _ := http.NewRequestWithContext(ctx, "POST", "http://fixture.invalid", nil)
			client := HTTPClient(roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, tc.cause }))
			_, err := client.Do(request)
			safe := Outcome("openwa", true, err, a)
			var outcome *Error
			if !errors.As(safe, &outcome) || outcome.Retry != tc.retry || outcome.Uncertain != tc.uncertain {
				t.Fatalf("outcome=%+v", safe)
			}
			if strings.Contains(fmt.Sprintf("%+v", safe), "private") {
				t.Fatal("network detail escaped safe error")
			}
		})
	}
}

func TestProviderURLNeverEmbedsCredentials(t *testing.T) {
	for _, value := range []string{"http://user:private@host", "file:///tmp/private", "https://host?key=private", "https://host/#private"} {
		if ValidURL(value) {
			t.Fatal("credential-bearing or non-HTTP URL accepted")
		}
	}
}
