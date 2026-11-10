package openwa

import (
	"context"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

// Group uses the native roster response. A missing participant array cannot
// establish membership, even when the HTTP response itself was successful.
func (c *Client) Group(ctx context.Context, session, chat string) (*sdk.GroupInfo, error) {
	if session == "" || chat == "" {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	r, err := c.sdk.Groups.Get(ctx, session, chat)
	if err := providers.Outcome("openwa", false, err, a); err != nil {
		return nil, err
	}
	if r == nil || r.ID != chat || r.Name == "" || r.Participants == nil {
		return nil, providers.InvalidResponse("openwa", false)
	}
	for _, m := range r.Participants {
		if !identity.IsPerson(m.ID) {
			return nil, providers.InvalidResponse("openwa", false)
		}
	}
	return r, nil
}

// Phone returns only independently resolved phone digits. ContactRecord.Number
// and message.contact.number can contain LID digits and are not evidence.
func (c *Client) Phone(ctx context.Context, session, id string) (*sdk.ContactPhoneResponse, error) {
	if session == "" || !identity.IsPerson(id) {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	r, err := c.sdk.Contacts.Phone(ctx, session, id)
	if err := providers.Outcome("openwa", false, err, a); err != nil {
		return nil, err
	}
	if r == nil || identity.Normalize(r.ContactID) != identity.Normalize(id) {
		return nil, providers.InvalidResponse("openwa", false)
	}
	if r.Phone != nil {
		if _, valid := identity.PhoneID(*r.Phone); !valid {
			return nil, providers.InvalidResponse("openwa", false)
		}
	}
	return r, nil
}
