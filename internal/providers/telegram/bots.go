package telegram

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func (c *Client) ManagedToken(ctx context.Context, p *bot.GetManagedBotTokenParams) (string, error) {
	if p == nil || p.UserID <= 0 {
		return "", providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	token, err := c.sdk.GetManagedBotToken(ctx, p)
	if err := providers.Outcome("telegram", false, err, a); err != nil {
		return "", err
	}
	if token == "" {
		return "", providers.InvalidResponse("telegram", false)
	}
	return token, nil
}
func (c *Client) LogOut(ctx context.Context) error {
	ctx, a := providers.Observe(ctx)
	ok, err := c.sdk.Logout(ctx)
	if err := providers.Outcome("telegram", true, err, a); err != nil {
		return err
	}
	if !ok {
		return providers.InvalidResponse("telegram", true)
	}
	return nil
}

// SetWebhook is an idempotent setting, journaled independently of message sends.
func (c *Client) SetWebhook(ctx context.Context, p *bot.SetWebhookParams) error {
	if p == nil || p.URL == "" || p.SecretToken == "" || p.DropPendingUpdates {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	ok, err := c.sdk.SetWebhook(ctx, p)
	if err := providers.Outcome("telegram", false, err, a); err != nil {
		return err
	}
	if !ok {
		return providers.InvalidResponse("telegram", false)
	}
	return nil
}
