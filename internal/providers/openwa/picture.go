package openwa

import (
	"context"
	"strings"

	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func (c *Client) PictureURL(ctx context.Context, session, id string) (*string, error) {
	if session == "" || !strings.HasSuffix(id, "@g.us") && !identity.IsPerson(id) {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	if strings.HasSuffix(id, "@g.us") {
		r, err := c.sdk.Groups.GetPicture(ctx, session, id)
		if err := providers.Outcome("openwa", false, err, a); err != nil {
			return nil, err
		}
		if r == nil {
			return nil, providers.InvalidResponse("openwa", false)
		}
		if r.URL == "" {
			return nil, nil
		}
		return &r.URL, nil
	}
	r, err := c.sdk.Contacts.ProfilePicture(ctx, session, id)
	if err := providers.Outcome("openwa", false, err, a); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, providers.InvalidResponse("openwa", false)
	}
	return r.URL, nil
}
