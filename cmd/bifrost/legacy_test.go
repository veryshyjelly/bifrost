package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/legacy"
)

func TestLegacyValidationCLIRequiresNoRuntimeConfigurationAndHidesPrivatePaths(t *testing.T) {
	t.Setenv("DATABASE_URL", "deliberately invalid and unused")
	var out bytes.Buffer
	if err := validateLegacyCLI(t.Context(), []string{"--help"}, &out); err != nil || !strings.Contains(out.String(), "legacy-validate --file") {
		t.Fatal("standalone help unavailable")
	}
	out.Reset()
	secretPath := "/private/nonexistent-export-with-secret-name.tar.gz"
	if err := validateLegacyCLI(t.Context(), []string{"--file", secretPath}, &out); !errors.Is(err, legacy.ErrInvalid) || strings.Contains(err.Error(), secretPath) || out.Len() != 0 {
		t.Fatal("failed validation exposed source path or emitted a success summary")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := validateLegacyCLI(ctx, []string{"--file", secretPath}, &out); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation not preserved")
	}
}
