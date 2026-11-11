package openwa

import (
	"context"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func (c *Client) History(ctx context.Context, session, chat string, count int) ([]sdk.ChatHistoryMessage, error) {
	if session == "" || chat == "" || count < 1 || count > 2000 {
		return nil, providers.ErrInvalidRequest
	}
	includeMedia, deep := false, count > 100
	ctx, a := providers.ObserveMedia(ctx, int64(count)*(1<<20))
	rows, err := c.sdk.Messages.History(ctx, session, chat, &sdk.MessageHistoryQuery{Limit: &count, IncludeMedia: &includeMedia, Deep: &deep})
	if err := providers.Outcome("openwa", false, err, a); err != nil {
		return nil, err
	}
	if rows == nil {
		return nil, providers.InvalidResponse("openwa", false)
	}
	return rows, nil
}
