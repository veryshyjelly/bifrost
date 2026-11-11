package attachments

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/providers"
	wa "github.com/veryshyjelly/bifrost/internal/providers/openwa"
)

type WhatsApp interface {
	StoredMessageMedia(context.Context, string, string, string, int64) (wa.StoredMedia, error)
	StoredStatusMedia(context.Context, string, string, int64) (wa.StoredMedia, error)
	SourceMedia(context.Context, string, string, string, int64) (*sdk.MessageMediaDownload, error)
}
type Telegram interface {
	File(context.Context, string) (*models.File, error)
	FileBody(context.Context, *models.File, int64) (io.ReadCloser, error)
}
type TelegramClients func(context.Context, string, string) (Telegram, error)
type Sources struct {
	assets                *Assets
	wa                    WhatsApp
	telegram              TelegramClients
	session, telegramRoot string
	max                   int64
}

func NewSources(assets *Assets, wa WhatsApp, telegram TelegramClients, session, telegramRoot string, max int64) (*Sources, error) {
	if assets == nil || wa == nil || telegram == nil || session == "" || telegramRoot == "" || max < 1024 || max > 1<<53-1 {
		return nil, providers.ErrInvalidRequest
	}
	return &Sources{assets: assets, wa: wa, telegram: telegram, session: session, telegramRoot: telegramRoot, max: max}, nil
}
func defaultName(kind sdk.MessageType) string {
	ext := map[sdk.MessageType]string{sdk.MsgImage: "jpg", sdk.MsgVideo: "mp4", sdk.MsgVoice: "ogg", sdk.MsgAudio: "mp3", sdk.MsgSticker: "webp"}[kind]
	if ext == "" {
		ext = "bin"
	}
	return "attachment." + ext
}
func (s *Sources) WhatsApp(ctx context.Context, m sdk.ChatHistoryMessage, statusID string) (Asset, error) {
	name, mime := defaultName(m.Type), "application/octet-stream"
	if m.Media != nil {
		if m.Media.Filename != "" {
			name = filepath.Base(m.Media.Filename)
		}
		if m.Media.Mimetype != "" {
			mime = m.Media.Mimetype
		}
		if m.Media.SizeBytes > s.max {
			return Asset{}, TooLarge()
		}
		if data := m.Media.Data; data != "" {
			if strings.HasPrefix(data, "data:") {
				if i := strings.IndexByte(data, ','); i >= 0 {
					data = data[i+1:]
				}
			}
			return s.assets.Save(ctx, base64.NewDecoder(base64.StdEncoding, strings.NewReader(data)), name, mime, s.max)
		}
		if statusID != "" && m.Media.Omitted {
			return Asset{}, &providers.Error{Service: "openwa", Status: 404, Code: "MEDIA_SOURCE_UNAVAILABLE"}
		}
	}
	var stored wa.StoredMedia
	var err error
	if statusID != "" {
		stored, err = s.wa.StoredStatusMedia(ctx, s.session, statusID, s.max)
	} else {
		stored, err = s.wa.StoredMessageMedia(ctx, s.session, m.ChatID, m.ID, s.max)
	}
	if err == nil {
		if stored.Filename != "" {
			name = stored.Filename
		}
		if stored.MIME != "" && stored.MIME != "application/octet-stream" {
			mime = stored.MIME
		}
		return s.assets.Save(ctx, bytes.NewReader(stored.Data), name, mime, s.max)
	}
	var refusal *providers.Error
	if statusID != "" || !errors.As(err, &refusal) || refusal.Status != 404 {
		return Asset{}, err
	}
	if refusal.Code == "MEDIA_SOURCE_UNAVAILABLE" {
		return Asset{}, err
	}
	if refusal.Code != "MEDIA_NOT_STORED" {
		return Asset{}, &providers.Error{Service: "openwa", Status: 502, Code: "STORED_MEDIA_ROUTE_MISSING", Retry: true}
	}
	source, err := s.wa.SourceMedia(ctx, s.session, m.ChatID, m.ID, s.max)
	if err != nil {
		if errors.As(err, &refusal) && refusal.Status == 404 && refusal.Code != "MEDIA_SOURCE_UNAVAILABLE" {
			return Asset{}, &providers.Error{Service: "openwa", Status: 502, Code: "SOURCE_MEDIA_ROUTE_MISSING", Retry: true}
		}
		return Asset{}, err
	}
	if source == nil || source.Body == nil {
		return Asset{}, providers.InvalidResponse("openwa", false)
	}
	defer source.Body.Close()
	if source.Filename != "" {
		name = source.Filename
	}
	if source.Mimetype != "" && source.Mimetype != "application/octet-stream" {
		mime = source.Mimetype
	} else if source.ContentType != "" && source.ContentType != "application/octet-stream" {
		mime = source.ContentType
	}
	return s.assets.Save(ctx, source.Body, name, mime, s.max)
}

type TelegramAttachment struct {
	Kind, FileID, Name, MIME string
	Size                     int64
}

func SelectTelegram(m *models.Message) (TelegramAttachment, bool) {
	if m == nil {
		return TelegramAttachment{}, false
	}
	var a TelegramAttachment
	switch {
	case len(m.Photo) > 0:
		p := m.Photo[len(m.Photo)-1]
		a = TelegramAttachment{Kind: "photo", FileID: p.FileID, MIME: "image/jpeg", Size: int64(p.FileSize)}
	case m.Video != nil:
		p := m.Video
		a = TelegramAttachment{Kind: "video", FileID: p.FileID, Name: p.FileName, MIME: p.MimeType, Size: int64(p.FileSize)}
	case m.Animation != nil:
		p := m.Animation
		a = TelegramAttachment{Kind: "animation", FileID: p.FileID, Name: p.FileName, MIME: p.MimeType, Size: int64(p.FileSize)}
	case m.Voice != nil:
		p := m.Voice
		a = TelegramAttachment{Kind: "voice", FileID: p.FileID, MIME: p.MimeType, Size: int64(p.FileSize)}
	case m.Audio != nil:
		p := m.Audio
		a = TelegramAttachment{Kind: "audio", FileID: p.FileID, Name: p.FileName, MIME: p.MimeType, Size: int64(p.FileSize)}
	case m.Document != nil:
		p := m.Document
		a = TelegramAttachment{Kind: "document", FileID: p.FileID, Name: p.FileName, MIME: p.MimeType, Size: int64(p.FileSize)}
	case m.Sticker != nil:
		p := m.Sticker
		mime := "image/webp"
		if p.IsAnimated {
			mime = "application/x-tgsticker"
		} else if p.IsVideo {
			mime = "video/webm"
		}
		a = TelegramAttachment{Kind: "sticker", FileID: p.FileID, MIME: mime, Size: int64(p.FileSize)}
	case m.VideoNote != nil:
		p := m.VideoNote
		a = TelegramAttachment{Kind: "video_note", FileID: p.FileID, MIME: "video/mp4", Size: int64(p.FileSize)}
	default:
		return a, false
	}
	if a.MIME == "" {
		a.MIME = map[string]string{"photo": "image/jpeg", "voice": "audio/ogg; codecs=opus", "video": "video/mp4", "animation": "video/mp4", "video_note": "video/mp4"}[a.Kind]
		if a.MIME == "" {
			a.MIME = "application/octet-stream"
		}
	}
	if a.Name == "" {
		ext := "bin"
		for _, part := range []struct{ match, ext string }{{"tgsticker", "tgs"}, {"webm", "webm"}, {"webp", "webp"}, {"ogg", "ogg"}, {"jpeg", "jpg"}, {"mp4", "mp4"}} {
			if strings.Contains(a.MIME, part.match) {
				ext = part.ext
				break
			}
		}
		a.Name = "telegram." + ext
	}
	return a, a.FileID != ""
}
func (s *Sources) Telegram(ctx context.Context, account, bot string, m *models.Message) (Asset, string, error) {
	a, ok := SelectTelegram(m)
	if !ok {
		return Asset{}, "", providers.ErrInvalidRequest
	}
	if a.Size > s.max {
		return Asset{}, a.Kind, TooLarge()
	}
	client, err := s.telegram(ctx, account, bot)
	if err != nil {
		return Asset{}, a.Kind, err
	}
	file, err := client.File(ctx, a.FileID)
	if err != nil {
		return Asset{}, a.Kind, err
	}
	if file == nil {
		return Asset{}, a.Kind, providers.InvalidResponse("telegram", false)
	}
	if int64(file.FileSize) > s.max {
		return Asset{}, a.Kind, TooLarge()
	}
	var reader io.ReadCloser
	if filepath.IsAbs(file.FilePath) {
		root, err := filepath.EvalSymlinks(s.telegramRoot)
		if err != nil {
			return Asset{}, a.Kind, ErrAsset
		}
		path, err := filepath.EvalSymlinks(file.FilePath)
		if err != nil {
			return Asset{}, a.Kind, ErrAsset
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return Asset{}, a.Kind, providers.ErrInvalidRequest
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return Asset{}, a.Kind, ErrAsset
		}
		if info.Size() > s.max {
			return Asset{}, a.Kind, TooLarge()
		}
		reader, err = os.Open(path)
		if err != nil {
			return Asset{}, a.Kind, ErrAsset
		}
	} else {
		reader, err = client.FileBody(ctx, file, s.max)
		if err != nil {
			return Asset{}, a.Kind, err
		}
	}
	defer reader.Close()
	asset, err := s.assets.Save(ctx, reader, a.Name, a.MIME, s.max)
	return asset, a.Kind, err
}
