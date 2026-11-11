package telegram

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func (c *Client) ProfilePhotos(ctx context.Context, p *bot.GetUserProfilePhotosParams) (*models.UserProfilePhotos, error) {
	if p == nil || p.UserID <= 0 || p.Limit != 1 {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.GetUserProfilePhotos(ctx, p)
	if err := providers.Outcome("telegram", false, err, a); err != nil {
		return nil, err
	}
	if result == nil || result.TotalCount < 0 || result.TotalCount > 0 && (len(result.Photos) == 0 || len(result.Photos[0]) == 0) {
		return nil, providers.InvalidResponse("telegram", false)
	}
	return result, nil
}

func (c *Client) SetChatPhoto(ctx context.Context, p *bot.SetChatPhotoParams) error {
	if p == nil || p.ChatID == nil || p.Photo == nil {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	ok, err := c.sdk.SetChatPhoto(ctx, p)
	if err := providers.Outcome("telegram", true, err, a); err != nil {
		return err
	}
	if !ok {
		return providers.InvalidResponse("telegram", true)
	}
	return nil
}

func (c *Client) SetMyProfilePhoto(ctx context.Context, p *bot.SetMyProfilePhotoParams) error {
	if p == nil || p.Photo == nil {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	ok, err := c.sdk.SetMyProfilePhoto(ctx, p)
	if err := providers.Outcome("telegram", true, err, a); err != nil {
		return err
	}
	if !ok {
		return providers.InvalidResponse("telegram", true)
	}
	return nil
}
