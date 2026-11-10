package telegram

import (
	"context"
	"net/http"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

type Client struct{ sdk *bot.Bot }

// New performs no remote requests, polling, webhook registration or token migration.
func New(baseURL, token string, transport http.RoundTripper) (*Client, error) {
	if !providers.ValidURL(baseURL) || token == "" {
		return nil, providers.ErrInvalidRequest
	}
	client, err := bot.New(token, bot.WithSkipGetMe(), bot.WithServerURL(baseURL),
		bot.WithHTTPClient(0, providers.HTTPClient(transport)), bot.WithErrorsHandler(func(error) {}))
	if err != nil {
		return nil, providers.ErrInvalidRequest
	}
	return &Client{sdk: client}, nil
}

func (c *Client) Me(ctx context.Context) (*models.User, error) {
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.GetMe(ctx)
	if err := providers.Outcome("telegram", false, err, a); err != nil {
		return nil, err
	}
	if result == nil || result.ID == 0 {
		return nil, providers.InvalidResponse("telegram", false)
	}
	return result, nil
}

func (c *Client) SendText(ctx context.Context, request *bot.SendMessageParams) (*models.Message, error) {
	if request == nil {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.SendMessage(ctx, request)
	if err := providers.Outcome("telegram", true, err, a); err != nil {
		return nil, err
	}
	if result == nil || result.ID == 0 {
		return nil, providers.InvalidResponse("telegram", true)
	}
	return result, nil
}
