package telegram

import (
	"context"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

// A repeated edit may return not_modified instead of a message receipt. Other
// successful responses must identify the original message before being accepted.
func editOutcome(err error, a *providers.Attempt, m *models.Message, id int) error {
	outcome := providers.Outcome("telegram", true, err, a)
	if e, ok := outcome.(*providers.Error); ok && e.Status == 400 && e.Reason == "not_modified" {
		return nil
	}
	if outcome != nil {
		return outcome
	}
	if m == nil || m.ID != id {
		return providers.InvalidResponse("telegram", true)
	}
	return nil
}
func (c *Client) EditText(ctx context.Context, p *bot.EditMessageTextParams) error {
	if p == nil || p.ChatID == nil || p.MessageID <= 0 || p.Text == "" || p.ParseMode != "" {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	m, err := c.sdk.EditMessageText(ctx, p)
	return editOutcome(err, a, m, p.MessageID)
}

// SetText replaces a frozen control view. Callers journal progress separately
// and can safely repeat the same replacement after a lost acknowledgement.
func (c *Client) SetText(ctx context.Context, p *bot.EditMessageTextParams) error {
	if p == nil || p.ChatID == nil || p.MessageID <= 0 || p.Text == "" || p.ParseMode != "" {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	m, err := c.sdk.EditMessageText(ctx, p)
	outcome := providers.Outcome("telegram", false, err, a)
	if e, ok := outcome.(*providers.Error); ok && e.Status == 400 && e.Reason == "not_modified" {
		return nil
	}
	if outcome != nil {
		return outcome
	}
	if m == nil || m.ID != p.MessageID {
		return providers.InvalidResponse("telegram", false)
	}
	return nil
}
func (c *Client) EditCaption(ctx context.Context, p *bot.EditMessageCaptionParams) error {
	if p == nil || p.ChatID == nil || p.MessageID <= 0 || p.ParseMode != "" {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	m, err := c.sdk.EditMessageCaption(ctx, p)
	return editOutcome(err, a, m, p.MessageID)
}
