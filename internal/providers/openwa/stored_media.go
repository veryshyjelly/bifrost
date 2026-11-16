package openwa

import (
	"context"
	"errors"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

// StoredMedia contains bounded gateway archive bytes and observed file metadata.
type StoredMedia struct {
	Data           []byte
	MIME, Filename string
}

func (c *Client) storedMedia(ctx context.Context, session, chat, id string, max int64, status bool) (StoredMedia, error) {
	if session == "" || id == "" || max <= 0 || (!status && chat == "") {
		return StoredMedia{}, providers.ErrInvalidRequest
	}
	ctx, a := providers.ObserveMedia(ctx, max)
	var data []byte
	var contentType string
	var err error
	if status {
		var out *sdk.StatusMedia
		out, err = c.sdk.Status.Media(ctx, session, id)
		if out != nil {
			data, contentType = out.Data, out.ContentType
		}
	} else {
		var out *sdk.MessageMedia
		out, err = c.sdk.Messages.Media(ctx, session, chat, id)
		if out != nil {
			data, contentType = out.Data, out.ContentType
		}
	}
	name, mime, exceeded := a.MediaMetadata()
	if exceeded {
		return StoredMedia{}, mediaTooLarge()
	}
	var api *sdk.APIError
	if errors.As(err, &api) && api.StatusCode == 404 && (api.Code == "MEDIA_NOT_STORED" || api.Message == "No media stored for this message") {
		return StoredMedia{}, &providers.Error{Service: "openwa", Status: 404, Code: "MEDIA_NOT_STORED"}
	}
	if err := providers.Outcome("openwa", false, err, a); err != nil {
		return StoredMedia{}, err
	}
	if len(data) == 0 {
		return StoredMedia{}, providers.InvalidResponse("openwa", false)
	}
	if int64(len(data)) > max {
		return StoredMedia{}, mediaTooLarge()
	}
	if mime == "" {
		mime = contentType
	}
	return StoredMedia{Data: data, MIME: mime, Filename: name}, nil
}
func (c *Client) StoredMessageMedia(ctx context.Context, session, chat, id string, max int64) (StoredMedia, error) {
	return c.storedMedia(ctx, session, chat, id, max, false)
}
func (c *Client) StoredStatusMedia(ctx context.Context, session, id string, max int64) (StoredMedia, error) {
	return c.storedMedia(ctx, session, "", id, max, true)
}
