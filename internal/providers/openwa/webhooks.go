package openwa

import (
	"context"
	"slices"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

// EnsureWebhook owns only the exact callback URL. It leaves unrelated
// subscriptions intact and refuses ambiguous duplicate ownership.
func (c *Client) EnsureWebhook(ctx context.Context, session, callback, secret string, events []string) error {
	if session == "" || !providers.ValidURL(callback) || len(secret) < 32 || len(events) == 0 {
		return providers.ErrInvalidRequest
	}
	ctx, attempt := providers.Observe(ctx)
	hooks, err := c.sdk.Webhooks.List(ctx, session)
	if err := providers.Outcome("openwa", false, err, attempt); err != nil {
		return err
	}
	id := ""
	for _, hook := range hooks {
		if hook.URL != callback {
			continue
		}
		if id != "" || hook.ID == "" || hook.SessionID != session {
			return providers.InvalidResponse("openwa", false)
		}
		id = hook.ID
	}
	retries, active := 5, true
	ctx, attempt = providers.Observe(ctx)
	var saved *sdk.WebhookResponse
	if id == "" {
		saved, err = c.sdk.Webhooks.Create(ctx, session, sdk.CreateWebhookRequest{URL: callback, Events: slices.Clone(events), Secret: secret, RetryCount: &retries})
	} else {
		headers := map[string]string{}
		saved, err = c.sdk.Webhooks.Update(ctx, session, id, sdk.UpdateWebhookRequest{Events: slices.Clone(events), Secret: &secret, Headers: &headers, ClearFilters: true, Active: &active, RetryCount: &retries})
	}
	// A lost settings response is recoverable by listing this exact URL again.
	if err := providers.Outcome("openwa", false, err, attempt); err != nil {
		return err
	}
	if saved == nil || saved.ID == "" || saved.SessionID != session || saved.URL != callback || !saved.Active || saved.Filters != nil || !sameWebhookEvents(saved.Events, events) {
		return providers.InvalidResponse("openwa", false)
	}
	return nil
}

func sameWebhookEvents(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
