package telegram

import (
	"context"
	"errors"

	"github.com/go-telegram/bot"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

// DeleteForumTopic deliberately excludes General. A missing topic is success;
// retrying the same reserved topic after a lost response is safe.
func (c *Client) DeleteForumTopic(ctx context.Context, p *bot.DeleteForumTopicParams) error {
	var chat any
	if p != nil {
		chat = p.ChatID
	}
	var id int64
	ok := false
	switch v := chat.(type) {
	case int64:
		id, ok = v, true
	case int:
		id, ok = int64(v), true
	}
	if p == nil || !ok || id >= 0 || p.MessageThreadID <= 1 {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	deleted, err := c.sdk.DeleteForumTopic(ctx, p)
	if err := providers.Outcome("telegram", false, err, a); err != nil {
		var refusal *providers.Error
		if errors.As(err, &refusal) && refusal.Status == 400 && refusal.Reason == "thread_missing" {
			return nil
		}
		return err
	}
	if !deleted {
		return providers.InvalidResponse("telegram", false)
	}
	return nil
}
