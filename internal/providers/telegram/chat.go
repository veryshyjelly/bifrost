package telegram

import (
	"context"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func (c *Client) Chat(ctx context.Context, p *bot.GetChatParams) (*models.ChatFullInfo, error) {
	if p == nil || p.ChatID == nil {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	chat, err := c.sdk.GetChat(ctx, p)
	if err := providers.Outcome("telegram", false, err, a); err != nil {
		return nil, err
	}
	if chat == nil || chat.ID == 0 || chat.Type == "" {
		return nil, providers.InvalidResponse("telegram", false)
	}
	return chat, nil
}
