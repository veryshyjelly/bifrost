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

func TestLegacyPreparationCLIHelpAndValidationDoNotRequireRuntimeSecrets(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	var out bytes.Buffer
	if err := prepareLegacyCLI(t.Context(), []string{"--help"}, &out); err != nil || !strings.Contains(out.String(), "never activates") {
		t.Fatal("standalone preparation help unavailable", err)
	}
	out.Reset()
	if err := prepareLegacyCLI(t.Context(), []string{"--file", "/private/source"}, &out); err == nil || out.Len() != 0 || strings.Contains(err.Error(), "/private/source") {
		t.Fatal("missing destination exposed a path or emitted success")
	}
}
