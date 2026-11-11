package telegram

import (
	"context"
	"github.com/go-telegram/bot"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

// DeleteMessages is idempotent: Telegram skips missing message IDs. Cleanup
// journals frozen targets separately from potentially uncertain message sends.
func (c *Client) DeleteMessages(ctx context.Context, p *bot.DeleteMessagesParams) error {
	if p == nil || len(p.MessageIDs) == 0 || len(p.MessageIDs) > 100 {
		return providers.ErrInvalidRequest
	}
	for _, id := range p.MessageIDs {
		if id <= 0 {
			return providers.ErrInvalidRequest
		}
	}
	ctx, a := providers.Observe(ctx)
	ok, err := c.sdk.DeleteMessages(ctx, p)
	if err := providers.Outcome("telegram", false, err, a); err != nil {
		return err
	}
	if !ok {
		return providers.InvalidResponse("telegram", false)
	}
	return nil
}
