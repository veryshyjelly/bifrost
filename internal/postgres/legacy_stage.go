package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/legacy"
)

type LegacyStaging struct {
	Preparation LegacyPreparation `json:"preparation"`
	Completed   []string          `json:"completed"`
	Ready       bool              `json:"ready"`
	QueueMode   string            `json:"queue_mode,omitempty"`
}

// LegacyStageError exposes only a capability name in its printable form.
// Callers may inspect the wrapped cause without logging private source data.
type LegacyStageError struct {
	Stage string
	Cause error
}

func (e *LegacyStageError) Error() string {
	return "legacy preparation stopped at " + e.Stage + "; preparation did not activate delivery"
}
func (e *LegacyStageError) Unwrap() error { return e.Cause }

// StageLegacyImport composes the implemented preparation stages. Each stage
// commits atomically and exact-archive replay resumes its immutable checkpoint.
// This is deliberately not activation or external asset restoration.
func (s *Store) StageLegacyImport(ctx context.Context, bundle *legacy.Bundle, options legacy.SQLiteOptions, session string) (LegacyStaging, error) {
	var result LegacyStaging
	if bundle == nil {
		return result, legacy.ErrInvalid
	}
	preparation, err := s.PrepareLegacyImport(ctx, bundle, options)
	if err != nil {
		return result, &LegacyStageError{Stage: "source", Cause: err}
	}
	result.Preparation = preparation
	if err := s.LegacyRuntimeReady(ctx, bundle.Account); !errors.Is(err, ErrLegacyPending) {
		if err == nil {
			err = legacy.ErrInvalid
		}
		return result, &LegacyStageError{Stage: "inactive-checkpoint", Cause: err}
	}
	key := bundle.Configuration().EncryptionKey()
	stages := []struct {
		name string
		run  func() error
	}{
		{"credentials", func() error { _, err := s.ProjectLegacyBots(ctx, bundle.Account, key); return err }},
		{"directory", func() error { _, err := s.ProjectLegacyDirectory(ctx, bundle.Account); return err }},
		{"routes", func() error { _, err := s.ProjectLegacyRoutes(ctx, bundle.Account, key, session); return err }},
		{"completions", func() error { _, err := s.ProjectLegacyCompletions(ctx, bundle.Account); return err }},
		{"messages", func() error { _, err := s.ProjectLegacyMessages(ctx, bundle.Account, key); return err }},
		{"sources", func() error { _, err := s.ProjectLegacySources(ctx, bundle.Account); return err }},
		{"changes", func() error { _, err := s.ProjectLegacyChanges(ctx, bundle.Account); return err }},
		{"interactions", func() error { _, err := s.ProjectLegacyInteractions(ctx, bundle.Account); return err }},
		{"incoming-reactions", func() error { _, err := s.ProjectLegacyIncomingReactions(ctx, bundle.Account, key); return err }},
		{"statuses", func() error { _, err := s.ProjectLegacyStatuses(ctx, bundle.Account, key); return err }},
	}
	for _, stage := range stages {
		if err := stage.run(); err != nil {
			return result, &LegacyStageError{Stage: stage.name, Cause: err}
		}
		result.Completed = append(result.Completed, stage.name)
	}
	if policy, err := s.queries.LegacyCutoverPolicy(ctx, bundle.Account); err == nil {
		if policy.ArchiveDigest != bundle.Digest {
			return result, &LegacyStageError{Stage: "queue-policy", Cause: legacy.ErrInvalid}
		}
		result.QueueMode = policy.QueueMode
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, &LegacyStageError{Stage: "queue-policy", Cause: err}
	}
	return result, nil
}
