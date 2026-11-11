package control

import (
	"context"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/presentation"
)

// Reply freezes a transient owner notice before sending it. The triggering
// Telegram update and all delivered reply parts retain their normal ledger.
func (s *Service) Reply(ctx context.Context, j delivery.Job, e *delivery.Effects, target Target, text presentation.Value) error {
	parts, err := presentation.Split(text, 3800)
	if err != nil {
		return err
	}
	p := Plan{Key: j.Dedup, Target: target, ReplyTTLSeconds: 60}
	for _, part := range parts {
		p.Packets = append(p.Packets, Packet{Text: part.Text, Entities: part.Entities})
	}
	p, err = s.store.PrepareControl(ctx, j, p)
	if err != nil {
		return local(err)
	}
	return s.Send(ctx, j, e, p)
}
