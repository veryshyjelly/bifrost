package openwa

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/metadata"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

// GroupMetadata uses the SDK's supported Do extension to retain an optional
// description. Groups.Get cannot distinguish absent/null from explicit empty
// in v0.11.1. Authentication, escaping and transport remain owned by the SDK.
func (c *Client) GroupMetadata(ctx context.Context, session, chat string) (*metadata.Group, error) {
	if session == "" || !strings.HasSuffix(chat, "@g.us") {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	var result metadata.Group
	err := c.sdk.Do(ctx, http.MethodGet, "/api/sessions/"+url.PathEscape(session)+"/groups/"+url.PathEscape(chat), nil, nil, &result)
	if err := providers.Outcome("openwa", false, err, a); err != nil {
		return nil, err
	}
	if result.ID != chat || strings.TrimSpace(result.Name) == "" || result.Participants == nil {
		return nil, providers.InvalidResponse("openwa", false)
	}
	for _, member := range result.Participants {
		if !identity.IsPerson(member.ID) {
			return nil, providers.InvalidResponse("openwa", false)
		}
	}
	return &result, nil
}

func (c *Client) Contact(ctx context.Context, session, chat string) (*sdk.ContactRecord, error) {
	if session == "" || !identity.IsPerson(chat) {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	result, err := c.sdk.Contacts.Get(ctx, session, chat)
	if err := providers.Outcome("openwa", false, err, a); err != nil {
		return nil, err
	}
	if result == nil || !identity.IsPerson(result.ID) {
		return nil, providers.InvalidResponse("openwa", false)
	}
	if result.ID != chat {
		if !strings.HasSuffix(chat, "@lid") || !strings.HasSuffix(result.ID, "@c.us") {
			return nil, providers.InvalidResponse("openwa", false)
		}
		proofCtx, proof := providers.Observe(ctx)
		resolved, err := c.sdk.Contacts.Phone(proofCtx, session, chat)
		if err := providers.Outcome("openwa", false, err, proof); err != nil {
			return nil, err
		}
		if resolved == nil || resolved.ContactID != chat || resolved.Phone == nil {
			return nil, providers.InvalidResponse("openwa", false)
		}
		phone, valid := identity.PhoneID(*resolved.Phone)
		if !valid || phone != result.ID {
			return nil, providers.InvalidResponse("openwa", false)
		}
		// Keep metadata attached to its requested identity. This proof does not
		// create a directory alias, change routing or interpret display numbers.
		copy := *result
		copy.ID = chat
		result = &copy
	}
	return result, nil
}
