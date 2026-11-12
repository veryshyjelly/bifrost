package openwa

import (
	"context"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func (c *Client) React(ctx context.Context, session string, p sdk.ReactMessageRequest) error {
	if session == "" || p.ChatID == "" || p.MessageID == "" {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.Messages.React(ctx, session, p)
	if err := providers.Outcome("openwa", false, err, a); err != nil {
		return err
	}
	if result == nil || !result.Success {
		return providers.InvalidResponse("openwa", false)
	}
	return nil
}
