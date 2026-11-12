package telegram

import (
	"context"
	"github.com/go-telegram/bot"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func (c *Client) EditMarkup(ctx context.Context, p *bot.EditMessageReplyMarkupParams) error {
	if p == nil || p.ChatID == nil || p.MessageID <= 0 || p.ReplyMarkup == nil {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.EditMessageReplyMarkup(ctx, p)
	outcome := providers.Outcome("telegram", false, err, a)
	if p, ok := outcome.(*providers.Error); ok && p.Status == 400 && p.Reason == "not_modified" {
		return nil
	}
	if outcome != nil {
		return outcome
	}
	if result == nil || result.ID != p.MessageID {
		return providers.InvalidResponse("telegram", false)
	}
	return nil
}
