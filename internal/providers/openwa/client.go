package openwa

import (
	"context"
	"net/http"
	"time"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

// Client wraps one gateway configuration; each operation supplies its session.
type Client struct{ sdk *sdk.Client }

// MarkRead is idempotent and retries as a read-state update, never as a send.
func (c *Client) MarkRead(ctx context.Context, session, chat string) error {
	if session == "" || chat == "" {
		return providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.Chats.MarkRead(ctx, session, sdk.MarkChatReadRequest{ChatID: chat})
	if err := providers.Outcome("openwa", false, err, a); err != nil {
		return err
	}
	if result == nil || !result.Success {
		return providers.InvalidResponse("openwa", false)
	}
	return nil
}

// New validates gateway configuration without contacting the gateway.
func New(baseURL, apiKey string, transport http.RoundTripper) (*Client, error) {
	if !providers.ValidURL(baseURL) || apiKey == "" {
		return nil, providers.ErrInvalidRequest
	}
	client, err := sdk.New(baseURL, apiKey, sdk.WithHTTPClient(providers.HTTPClient(transport)), sdk.WithTimeout(120*time.Second))
	if err != nil {
		return nil, providers.ErrInvalidRequest
	}
	return &Client{sdk: client}, nil
}

func (c *Client) Session(ctx context.Context, session string) (*sdk.SessionResponse, error) {
	if session == "" {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.Sessions.Get(ctx, session)
	if err := providers.Outcome("openwa", false, err, a); err != nil {
		return nil, err
	}
	if result == nil || result.ID == "" || result.Status == "" {
		return nil, providers.InvalidResponse("openwa", false)
	}
	return result, nil
}

// SendText sends with the saved effect key and requires a native message receipt.
// A missing receipt is an uncertain mutation, even after a successful HTTP status.
func (c *Client) SendText(ctx context.Context, session string, request sdk.SendTextRequest, key string) (*sdk.MessageResponse, error) {
	if session == "" || request.ChatID == "" || !providers.ValidKey(key) {
		return nil, providers.ErrInvalidRequest
	}
	ctx = sdk.WithIdempotencyKey(ctx, key)
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.Messages.SendText(ctx, session, request)
	if err := providers.Outcome("openwa", true, err, a); err != nil {
		return nil, err
	}
	if result == nil || result.MessageID == "" {
		return nil, providers.InvalidResponse("openwa", true)
	}
	return result, nil
}

func (c *Client) StoredMessages(ctx context.Context, session string, query *sdk.ListMessagesQuery) (*sdk.MessageListResponse, error) {
	if session == "" {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.Messages.List(ctx, session, query)
	if err := providers.Outcome("openwa", false, err, a); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, providers.InvalidResponse("openwa", false)
	}
	return result, nil
}

// Edit transmits an explicit empty body when requested; provider refusal must preserve the caption.
func (c *Client) Edit(ctx context.Context, session string, request sdk.EditMessageRequest) (*sdk.MessageResponse, error) {
	if session == "" || request.ChatID == "" || request.MessageID == "" {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.Messages.EditMessage(ctx, session, request)
	if err := providers.Outcome("openwa", true, err, a); err != nil {
		return nil, err
	}
	if result == nil || result.MessageID == "" {
		return nil, providers.InvalidResponse("openwa", true)
	}
	return result, nil
}
