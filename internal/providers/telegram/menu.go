package telegram

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

// Menu settings overwrite the same values and can retry a lost response. Their
// caller journals original settings and completion separately from sends.
func (c *Client) SetMyCommands(ctx context.Context, p *bot.SetMyCommandsParams) error {
	if p == nil || len(p.Commands) > 100 {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	ok, err := c.sdk.SetMyCommands(ctx, p)
	if err := providers.Outcome("telegram", false, err, a); err != nil {
		return err
	}
	if !ok {
		return providers.InvalidResponse("telegram", false)
	}
	return nil
}
func (c *Client) SetChatMenuButton(ctx context.Context, p *bot.SetChatMenuButtonParams) error {
	if p == nil || p.MenuButton == nil {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	ok, err := c.sdk.SetChatMenuButton(ctx, p)
	if err := providers.Outcome("telegram", false, err, a); err != nil {
		return err
	}
	if !ok {
		return providers.InvalidResponse("telegram", false)
	}
	return nil
}
