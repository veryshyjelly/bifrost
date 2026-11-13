package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

// Keep failure causes actionable without logging SDK errors or source content.
func runtimeDiagnosticHandlers(handlers map[string]delivery.Handler, logger *slog.Logger) map[string]delivery.Handler {
	result := make(map[string]delivery.Handler, len(handlers))
	for kind, handler := range handlers {
		result[kind] = func(ctx context.Context, job delivery.Job, effects *delivery.Effects) error {
			err := handler(ctx, job, effects)
			if err == nil {
				return nil
			}
			var failure *providers.Error
			if errors.As(err, &failure) {
				logger.Warn("delivery provider failure", "job", job.ID, "provider", failure.Service, "operation", failure.Operation, "status", failure.Status, "reason", failure.Reason, "retryable", failure.Retry, "uncertain", failure.Uncertain)
			} else {
				reason := "local"
				if errors.Is(err, delivery.ErrInvalid) {
					reason = "invalid"
				}
				if errors.Is(err, delivery.ErrUnsupported) {
					reason = "unsupported"
				}
				logger.Warn("delivery handler failure", "job", job.ID, "kind", job.Kind, "reason", reason)
			}
			return err
		}
	}
	return result
}
