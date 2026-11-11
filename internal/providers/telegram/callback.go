package telegram

import (
	"context"
	"github.com/go-telegram/bot"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

// AnswerCallback is an idempotent UI acknowledgement. A retry cannot duplicate
// a chat message; an expired-query 400 is handled by the control capability.
func (c *Client) AnswerCallback(ctx context.Context, p *bot.AnswerCallbackQueryParams) error {
	if p == nil || p.CallbackQueryID == "" {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	ok, err := c.sdk.AnswerCallbackQuery(ctx, p)
	if err := providers.Outcome("telegram", false, err, a); err != nil {
		return err
	}
	if !ok {
		return providers.InvalidResponse("telegram", false)
	}
	return nil
}
