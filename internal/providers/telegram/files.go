package telegram

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func (c *Client) File(ctx context.Context, id string) (*models.File, error) {
	if id == "" {
		return nil, providers.ErrInvalidRequest
	}
	ctx, a := providers.Observe(ctx)
	out, err := c.sdk.GetFile(ctx, &bot.GetFileParams{FileID: id})
	if err := providers.Outcome("telegram", false, err, a); err != nil {
		return nil, err
	}
	if out == nil || out.FileID == "" || out.FilePath == "" {
		return nil, providers.InvalidResponse("telegram", false)
	}
	return out, nil
}

// FileBody downloads a relative API file path with a byte limit. Absolute local
// API paths are handled by the archive role instead. The caller must close the body.
func (c *Client) FileBody(ctx context.Context, file *models.File, max int64) (io.ReadCloser, error) {
	if file == nil || file.FilePath == "" || strings.HasPrefix(file.FilePath, "/") || max <= 0 {
		return nil, providers.ErrInvalidRequest
	}
	parts := strings.Split(file.FilePath, "/")
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, providers.ErrInvalidRequest
		}
		parts[i] = url.PathEscape(part)
	}
	copy := *file
	copy.FilePath = strings.Join(parts, "/")
	link := c.sdk.FileDownloadLink(&copy)
	call, a := providers.ObserveMedia(ctx, max)
	request, err := http.NewRequestWithContext(call, http.MethodGet, link, nil)
	if err != nil {
		return nil, providers.ErrInvalidRequest
	}
	response, err := c.http.Do(request)
	if err != nil {
		_, _, large := a.MediaMetadata()
		if large {
			return nil, &providers.Error{Service: "telegram", Status: 413, Code: "MEDIA_TOO_LARGE"}
		}
		return nil, providers.Outcome("telegram", false, err, a)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, providers.Outcome("telegram", false, providers.ErrInvalidRequest, a)
	}
	return &telegramFileBody{ReadCloser: response.Body}, nil
}

type telegramFileBody struct{ io.ReadCloser }

func (b *telegramFileBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == providers.ErrMediaLimit {
		return n, &providers.Error{Service: "telegram", Status: 413, Code: "MEDIA_TOO_LARGE"}
	}
	return n, err
}
