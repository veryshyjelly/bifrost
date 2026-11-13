package telegram

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

type Client struct {
	sdk  *bot.Bot
	http *http.Client
}

// NewCloud retains the same SDK and delivery classification, using HTTP/2
// without sharing connections or allowing replayable mutation bodies.
func NewCloud(baseURL, token string, transport http.RoundTripper) (*Client, error) {
	if transport == nil && strings.HasPrefix(baseURL, "https://") {
		transport = providers.CloudTransport()
	}
	return New(baseURL, token, transport)
}

// New performs no remote requests, polling, webhook registration or token migration.
func New(baseURL, token string, transport http.RoundTripper) (*Client, error) {
	if !providers.ValidURL(baseURL) || token == "" {
		return nil, providers.ErrInvalidRequest
	}
	httpClient := providers.HTTPClient(transport)
	client, err := bot.New(token, bot.WithSkipGetMe(), bot.WithServerURL(baseURL),
		bot.WithHTTPClient(0, httpClient), bot.WithErrorsHandler(func(error) {}))
	if err != nil {
		return nil, providers.ErrInvalidRequest
	}
	return &Client{sdk: client, http: httpClient}, nil
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
	if keyboard, ok := request.ReplyMarkup.(*models.ReplyKeyboardMarkup); ok && keyboard != nil {
		copy := *request
		copy.ReplyMarkup = privateKeyboard(*keyboard)
		request = &copy
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

func (c *Client) CreateForumTopic(ctx context.Context, request *bot.CreateForumTopicParams) (*models.ForumTopic, error) {
	if request == nil || request.Name == "" {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.CreateForumTopic(ctx, request)
	if err := providers.Outcome("telegram", true, err, a); err != nil {
		return nil, err
	}
	if result == nil || result.MessageThreadID <= 0 {
		return nil, providers.InvalidResponse("telegram", true)
	}
	return result, nil
}

func (c *Client) ChatMember(ctx context.Context, request *bot.GetChatMemberParams) (*models.ChatMember, error) {
	if request == nil || request.UserID <= 0 {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.GetChatMember(ctx, request)
	if err := providers.Outcome("telegram", false, err, a); err != nil {
		return nil, err
	}
	if result == nil || result.Type == "" {
		return nil, providers.InvalidResponse("telegram", false)
	}
	return result, nil
}
