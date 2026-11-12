package openwa

import (
	"context"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

// VotePoll sets the complete selection. Repeating the same frozen request is
// safe; a transport interruption is retryable rather than an uncertain send.
func (c *Client) VotePoll(ctx context.Context, session string, p sdk.VotePollRequest) error {
	if session == "" || p.ChatID == "" || p.PollMessageID == "" || len(p.Options) > 12 {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.Messages.VotePoll(ctx, session, p)
	if err := providers.Outcome("openwa", false, err, a); err != nil {
		return err
	}
	if result == nil || !result.Success {
		return providers.InvalidResponse("openwa", false)
	}
	return nil
}
