package postgres

import (
	"context"
	"encoding/json"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/relay"
	"reflect"
)

func (s *Store) RefuseOutgoingPacket(ctx context.Context, j delivery.Job, key string) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		owner, err := q.NativeDeliveryPlan(ctx, db.NativeDeliveryPlanParams{AccountID: row.AccountID, PlanKey: key})
		if err != nil {
			return err
		}
		p, err := loadRelay(ctx, q, row.AccountID, key)
		if err != nil {
			return err
		}
		if owner.JobID != row.ID || p.Direction != "out" || p.Outgoing == nil || p.Outgoing.Kind != "sticker" || p.OutgoingFallback == nil || p.OutgoingFallback.Kind != "document" {
			return delivery.ErrInvalid
		}
		if p.UseOutgoingFallback {
			return nil
		}
		effectKey, request, err := p.FrozenRequest(0)
		if err != nil {
			return err
		}
		effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: effectKey})
		if err != nil {
			return err
		}
		var actual, expected relay.WhatsAppRequest
		if effect.JobID != row.ID || effect.State != "pending" || json.Unmarshal(effect.Request, &actual) != nil || json.Unmarshal(request, &expected) != nil || !reflect.DeepEqual(actual, expected) {
			return delivery.ErrUncertain
		}
		if _, err := q.DiscardEffect(ctx, db.DiscardEffectParams{AccountID: row.AccountID, JobID: row.ID, EffectKey: effectKey}); err != nil {
			return err
		}
		p.UseOutgoingFallback = true
		payload, err := json.Marshal(p)
		if err != nil {
			return err
		}
		return q.UpdateNativePlan(ctx, db.UpdateNativePlanParams{AccountID: row.AccountID, PlanKey: key, Payload: payload})
	})
}
