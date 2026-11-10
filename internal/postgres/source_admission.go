package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

// Source preparation and admission commit together. A definite identity/data
// refusal retains the original event for worker review without committing any
// partial alias/profile changes. Database failures withhold acknowledgement.
func (s *Store) admitWhatsApp(ctx context.Context, outer pgx.Tx, event delivery.Event) (bool, error) {
	sp, err := outer.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer sp.Rollback(ctx)
	q := s.queries.WithTx(sp)
	var source identity.Source
	prepared := false
	native, err := identity.DecodeSource(delivery.Job{Event: event})
	if err == nil {
		if err := q.LockQueueTopology(ctx); err != nil {
			return false, err
		}
		source, err = prepareSource(ctx, q, event.AccountID, native.Message)
		prepared = err == nil
	}
	if err != nil {
		if !errors.Is(err, delivery.ErrInvalid) && !errors.Is(err, delivery.ErrUnsupported) && !errors.Is(err, identity.ErrConflict) {
			return false, err
		}
		if err := sp.Rollback(ctx); err != nil {
			return false, err
		}
		_, err := s.queries.WithTx(outer).Enqueue(ctx, db.EnqueueParams{AccountID: event.AccountID, Dedup: event.Dedup, Stream: event.Stream, Kind: event.Kind, Payload: event.Payload})
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return err == nil, err
	}
	if prepared {
		event.Stream = "wa:" + source.ChatID
	}
	id, err := q.Enqueue(ctx, db.EnqueueParams{AccountID: event.AccountID, Dedup: event.Dedup, Stream: event.Stream, Kind: event.Kind, Payload: event.Payload})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if prepared {
		payload, err := json.Marshal(source)
		if err != nil {
			return false, err
		}
		if err := q.SaveWhatsAppSource(ctx, db.SaveWhatsAppSourceParams{AccountID: event.AccountID, JobID: id, WhatsappChat: source.ChatID, Payload: payload}); err != nil {
			return false, err
		}
	}
	return true, sp.Commit(ctx)
}
