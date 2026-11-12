package relay

import (
	"context"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/statuses"
)

func (n *Native) DeliverStatus(ctx context.Context, j delivery.Job, e *delivery.Effects, feed statuses.Feed, snapshot statuses.Snapshot) error {
	if e == nil || j.Kind != "wa" || feed.ThreadID <= 1 || snapshot.Skip || snapshot.Post.ID == "" {
		return delivery.ErrInvalid
	}
	key := NativeIncomingKey(statuses.ChatID, snapshot.Post.ID)
	if p, found, err := n.store.NativePlan(ctx, j.AccountID, key); err != nil {
		return local(err)
	} else if found {
		return n.send(ctx, j, e, p)
	}
	if feed.Management != n.c.Management {
		return delivery.ErrInvalid
	}
	p := NativePlan{Key: key, Direction: "in", Format: "native-packets-v1", Prefix: j.Dedup + ":native", Origin: "status", Session: n.c.Session, OwnerID: n.c.OwnerID, WhatsAppChat: statuses.ChatID, WhatsAppMessage: snapshot.Post.ID, BotID: "master", TelegramChat: feed.Management, ThreadID: feed.ThreadID, Source: snapshot.Source, Message: snapshot.Post.Message(), ControlTTL: n.c.ControlTTL, Presentation: presentation.Options{Status: true}}
	if err := n.render(ctx, j, &p); err != nil {
		return err
	}
	p, err := n.store.PrepareNative(ctx, j, p)
	if err != nil {
		return local(err)
	}
	return n.send(ctx, j, e, p)
}

var _ statuses.Archive = (*Native)(nil)
