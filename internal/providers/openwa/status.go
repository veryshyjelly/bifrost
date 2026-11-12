package openwa

import (
	"context"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func (c *Client) ListStatuses(ctx context.Context, session string) (*sdk.StatusListResponse, error) {
	if session == "" {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	r, err := c.sdk.Status.List(ctx, session)
	if err := providers.Outcome("openwa", false, err, a); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, providers.InvalidResponse("openwa", false)
	}
	return &sdk.StatusListResponse{Statuses: r}, nil
}
