package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func TestRuntimeDiagnosticsPreserveErrorAndNeverLogPrivateErrorsOrPayloads(t *testing.T) {
	for _, original := range []error{&providers.Error{Service: "telegram", Status: 400, Reason: "user_missing"}, errors.New("private SDK error and token"), delivery.ErrInvalid} {
		var out bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&out, nil))
		handlers := runtimeDiagnosticHandlers(map[string]delivery.Handler{"history-message": func(context.Context, delivery.Job, *delivery.Effects) error { return original }}, logger)
		err := handlers["history-message"](t.Context(), delivery.Job{ID: 603, Kind: "history-message", Payload: []byte(`"private message"`)}, nil)
		if err != original || strings.Contains(out.String(), "private") || !strings.Contains(out.String(), `"job":603`) {
			t.Fatal("unsafe or changed diagnostic", out.String(), err)
		}
	}
}
