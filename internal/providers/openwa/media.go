package openwa

import (
	"context"
	"errors"
	"io"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

// SourceMedia is an explicit read, used only after a documented stored-media miss.
// The caller must close Body and discard partial output on any read failure.
func (c *Client) SourceMedia(ctx context.Context, session, chat, message string, maxBytes int64) (*sdk.MessageMediaDownload, error) {
	if ctx == nil || session == "" || chat == "" || message == "" || maxBytes <= 0 || maxBytes > 1<<53-1 {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.ObserveStream(ctx)
	result, err := c.sdk.Messages.DownloadMedia(ctx, session, chat, message, maxBytes)
	if errors.Is(err, sdk.ErrMediaTooLarge) {
		return nil, mediaTooLarge()
	}
	var api *sdk.APIError
	if errors.As(err, &api) {
		switch {
		case api.Code == "MEDIA_DOWNLOAD_DISABLED":
			return nil, &providers.Error{Service: "openwa", Status: api.StatusCode, Code: "MEDIA_DOWNLOAD_DISABLED"}
		case api.StatusCode == 413:
			return nil, mediaTooLarge()
		case api.StatusCode == 501:
			return nil, &providers.Error{Service: "openwa", Status: 501, Code: "MEDIA_UNSUPPORTED"}
		case api.StatusCode == 404 && api.Code != "MEDIA_SOURCE_UNAVAILABLE" && api.Code != "MEDIA_NOT_STORED":
			return nil, &providers.Error{Service: "openwa", Status: 404, Code: "MEDIA_ROUTE_UNAVAILABLE", Retry: true}
		}
	}
	if err := providers.Outcome("openwa", false, err, a); err != nil {
		return nil, err
	}
	if result == nil || result.Body == nil {
		return nil, providers.InvalidResponse("openwa", false)
	}
	result.Body = &mediaBody{ReadCloser: result.Body}
	return result, nil
}

func mediaTooLarge() error {
	return &providers.Error{Service: "openwa", Status: 413, Code: "MEDIA_TOO_LARGE"}
}

type mediaBody struct{ io.ReadCloser }

func (b *mediaBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if errors.Is(err, sdk.ErrMediaTooLarge) {
		return n, mediaTooLarge()
	}
	if err != nil && err != io.EOF {
		return n, &providers.Error{Service: "openwa", Code: "MEDIA_READ_INTERRUPTED", Retry: true}
	}
	return n, err
}
func (b *mediaBody) Close() error {
	if err := b.ReadCloser.Close(); err != nil {
		return &providers.Error{Service: "openwa", Code: "MEDIA_CLOSE_FAILED", Retry: true}
	}
	return nil
}
