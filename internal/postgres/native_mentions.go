package postgres

import (
	"context"
	"encoding/json"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

func (s *Store) PrepareNativeMentionFallback(ctx context.Context, j delivery.Job, key string, part int) (relay.NativePlan, error) {
	var result relay.NativePlan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		owner, p, err := nativePlan(ctx, q, row.AccountID, key)
		if err != nil {
			return err
		}
		if owner.JobID != row.ID || p.Complete || part < 0 || part >= len(p.Packets) {
			return delivery.ErrInvalid
		}
		fallback, ok := relay.BotMentionLinkFallback(p.Packets[part])
		if !ok {
			return delivery.ErrInvalid
		}
		effectKey, request, err := p.FrozenPacket(part, false)
		if err != nil {
			return err
		}
		effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: effectKey})
		if err != nil {
			return err
		}
		if effect.JobID != row.ID || effect.State != "pending" || !sameNativePacket(effect.Request, request) {
			return delivery.ErrUncertain
		}
		unknown, err := q.HasUncertainEffect(ctx, row.ID)
		if err != nil {
			return err
		}
		if unknown {
			return delivery.ErrUncertain
		}
		sending, err := q.HasSendingEffect(ctx, row.ID)
		if err != nil {
			return err
		}
		if sending {
			return delivery.ErrInFlight
		}
		if _, err := q.DiscardEffect(ctx, db.DiscardEffectParams{AccountID: row.AccountID, JobID: row.ID, EffectKey: effectKey}); err != nil {
			return err
		}
		p.Packets[part].Fallback, p.Packets[part].UseFallback = &fallback, true
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if err := q.UpdateNativePlan(ctx, db.UpdateNativePlanParams{AccountID: row.AccountID, PlanKey: key, Payload: raw}); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}
