package openwa

import (
	"context"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func send(ctx context.Context, session, key string, call func(context.Context) (*sdk.MessageResponse, error)) (*sdk.MessageResponse, error) {
	if session == "" || !providers.ValidKey(key) {
		return nil, providers.ErrInvalidRequest
	}
	ctx = sdk.WithIdempotencyKey(ctx, key)
	ctx, a := providers.Observe(ctx)
	result, err := call(ctx)
	if err := providers.Outcome("openwa", true, err, a); err != nil {
		return nil, err
	}
	if result == nil || result.MessageID == "" {
		return nil, providers.InvalidResponse("openwa", true)
	}
	return result, nil
}
func (c *Client) SendMedia(ctx context.Context, session, kind string, body sdk.SendMediaRequest, key string) (*sdk.MessageResponse, error) {
	if body.ChatID == "" || body.Base64 == "" || body.Mimetype == "" || body.URL != "" {
		return nil, providers.ErrInvalidRequest
	}
	var call func(context.Context) (*sdk.MessageResponse, error)
	switch kind {
	case "photo":
		call = func(ctx context.Context) (*sdk.MessageResponse, error) {
			return c.sdk.Messages.SendImage(ctx, session, body)
		}
	case "video":
		call = func(ctx context.Context) (*sdk.MessageResponse, error) {
			return c.sdk.Messages.SendVideo(ctx, session, body)
		}
	case "document":
		call = func(ctx context.Context) (*sdk.MessageResponse, error) {
			return c.sdk.Messages.SendDocument(ctx, session, body)
		}
	case "sticker":
		call = func(ctx context.Context) (*sdk.MessageResponse, error) {
			return c.sdk.Messages.SendSticker(ctx, session, body)
		}
	default:
		return nil, providers.ErrInvalidRequest
	}
	return send(ctx, session, key, call)
}
func (c *Client) SendAudio(ctx context.Context, session string, body sdk.SendAudioRequest, key string) (*sdk.MessageResponse, error) {
	if body.ChatID == "" || body.Base64 == "" || body.Mimetype == "" || body.URL != "" {
		return nil, providers.ErrInvalidRequest
	}
	return send(ctx, session, key, func(ctx context.Context) (*sdk.MessageResponse, error) {
		return c.sdk.Messages.SendAudio(ctx, session, body)
	})
}
func (c *Client) SendLocation(ctx context.Context, session string, body sdk.SendLocationRequest, key string) (*sdk.MessageResponse, error) {
	if body.ChatID == "" {
		return nil, providers.ErrInvalidRequest
	}
	return send(ctx, session, key, func(ctx context.Context) (*sdk.MessageResponse, error) {
		return c.sdk.Messages.SendLocation(ctx, session, body)
	})
}
func (c *Client) SendContact(ctx context.Context, session string, body sdk.SendContactRequest, key string) (*sdk.MessageResponse, error) {
	if body.ChatID == "" || body.ContactNumber == "" {
		return nil, providers.ErrInvalidRequest
	}
	return send(ctx, session, key, func(ctx context.Context) (*sdk.MessageResponse, error) {
		return c.sdk.Messages.SendContact(ctx, session, body)
	})
}
func (c *Client) SendPoll(ctx context.Context, session string, body sdk.SendPollRequest, key string) (*sdk.MessageResponse, error) {
	if body.ChatID == "" || len(body.Options) < 2 || len(body.Options) > 12 {
		return nil, providers.ErrInvalidRequest
	}
	return send(ctx, session, key, func(ctx context.Context) (*sdk.MessageResponse, error) {
		return c.sdk.Messages.SendPoll(ctx, session, body)
	})
}
