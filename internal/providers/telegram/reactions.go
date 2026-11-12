package telegram

import (
	"context"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func (c *Client) SetReaction(ctx context.Context, p *bot.SetMessageReactionParams) error {
	if p == nil || p.ChatID == nil || p.MessageID <= 0 || len(p.Reaction) > 1 {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	success, err := c.sdk.SetMessageReaction(ctx, p)
	outcome := providers.Outcome("telegram", false, err, a)
	if e, ok := outcome.(*providers.Error); ok && e.Status == 400 && e.Reason == "not_modified" {
		return nil
	}
	if err := outcome; err != nil {
		return err
	}
	if !success {
		return providers.InvalidResponse("telegram", false)
	}
	return nil
}
func (c *Client) CustomEmoji(ctx context.Context, id string) ([]*models.Sticker, error) {
	if id == "" {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.GetCustomEmojiStickers(ctx, &bot.GetCustomEmojiStickersParams{CustomEmojiIDs: []string{id}})
	if err := providers.Outcome("telegram", false, err, a); err != nil {
		return nil, err
	}
	return result, nil
}
