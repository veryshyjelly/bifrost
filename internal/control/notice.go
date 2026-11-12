package control

import (
	"context"
	"encoding/json"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/presentation"
)

type Notice struct {
	Target     Target
	Body       presentation.Value
	ReplyTo    int64
	TTLSeconds int
}

func (n Notice) Plan(key string) (Plan, error) {
	if key == "" || n.Target.BotID == "" || n.Target.ChatID == 0 || n.Target.ThreadID < 0 || n.ReplyTo < 0 || n.TTLSeconds < 5 || n.TTLSeconds > 86400 {
		return Plan{}, delivery.ErrInvalid
	}
	parts, err := presentation.Split(n.Body, 3800)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Key: key, Target: n.Target, ReplyTTLSeconds: n.TTLSeconds}
	for _, part := range parts {
		if part.Text != "" {
			p.Packets = append(p.Packets, Packet{Text: part.Text, Entities: part.Entities, ReplyTo: n.ReplyTo})
		}
	}
	if len(p.Packets) == 0 {
		return Plan{}, delivery.ErrInvalid
	}
	return p, nil
}
func (s *Service) Notice(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "control-notice" || e == nil {
		return delivery.ErrInvalid
	}
	var n Notice
	if json.Unmarshal(j.Payload, &n) != nil {
		return delivery.ErrInvalid
	}
	p, err := n.Plan(j.Dedup)
	if err != nil {
		return err
	}
	p, err = s.store.PrepareControl(ctx, j, p)
	if err != nil {
		return local(err)
	}
	return s.Send(ctx, j, e, p)
}
