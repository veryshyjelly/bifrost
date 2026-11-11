package telegram

import (
	"context"
	"errors"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

// Metadata setters are idempotent: retrying a lost response restores the same
// frozen value. Telegram's already-set response is success for these setters.
func metadataResult(ok bool, err error, a *providers.Attempt) error {
	if err := providers.Outcome("telegram", false, err, a); err != nil {
		var refusal *providers.Error
		if errors.As(err, &refusal) && refusal.Status == 400 && refusal.Reason == "not_modified" {
			return nil
		}
		return err
	}
	if !ok {
		return providers.InvalidResponse("telegram", false)
	}
	return nil
}

func (c *Client) EditForumTopic(ctx context.Context, p *bot.EditForumTopicParams) error {
	if p == nil || p.ChatID == nil || p.MessageThreadID <= 1 || p.Name == "" {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	ok, err := c.sdk.EditForumTopic(ctx, p)
	return metadataResult(ok, err, a)
}
func (c *Client) SetChatTitle(ctx context.Context, p *bot.SetChatTitleParams) error {
	if p == nil || p.ChatID == nil || p.Title == "" {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	ok, err := c.sdk.SetChatTitle(ctx, p)
	return metadataResult(ok, err, a)
}
func (c *Client) SetChatDescription(ctx context.Context, p *bot.SetChatDescriptionParams) error {
	if p == nil || p.ChatID == nil {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	ok, err := c.sdk.SetChatDescription(ctx, p)
	return metadataResult(ok, err, a)
}
func (c *Client) SetMyName(ctx context.Context, p *bot.SetMyNameParams) error {
	if p == nil || p.Name == "" {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	ok, err := c.sdk.SetMyName(ctx, p)
	return metadataResult(ok, err, a)
}
func (c *Client) SetMyDescription(ctx context.Context, p *bot.SetMyDescriptionParams) error {
	if p == nil {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	ok, err := c.sdk.SetMyDescription(ctx, p)
	return metadataResult(ok, err, a)
}
func (c *Client) MyDescription(ctx context.Context) (*models.BotDescription, error) {
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.GetMyDescription(ctx, &bot.GetMyDescriptionParams{})
	if err := providers.Outcome("telegram", false, err, a); err != nil {
		return nil, err
	}
	return &result, nil
}
