package telegram

import (
	"context"
	"io"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/providers"
	"github.com/veryshyjelly/bifrost/internal/relay"
)

// SendPacket translates a frozen relay packet into one native send. The caller
// supplies the saved asset bytes through data and journals the returned receipt.
func (c *Client) SendPacket(ctx context.Context, p relay.NativePacket, data io.Reader) (*models.Message, error) {
	if p.Target.ChatID == 0 || p.Target.ThreadID < 0 {
		return nil, providers.ErrInvalidRequest
	}
	var reply *models.ReplyParameters
	if p.ReplyTo > 0 {
		reply = &models.ReplyParameters{MessageID: int(p.ReplyTo), AllowSendingWithoutReply: true}
	}
	if p.Kind == "text" {
		request := &bot.SendMessageParams{ChatID: p.Target.ChatID, MessageThreadID: int(p.Target.ThreadID), Text: p.Body.Text, Entities: p.Body.Entities, ReplyParameters: reply}
		if p.Markup != nil {
			request.ReplyMarkup = p.Markup
		}
		for _, entity := range p.Body.Entities {
			if entity.Type == models.MessageEntityTypeTextLink {
				request.LinkPreviewOptions = &models.LinkPreviewOptions{IsDisabled: bot.True()}
				break
			}
		}
		return c.SendText(ctx, request)
	}
	var file models.InputFile
	if p.Asset != nil && data != nil {
		file = &models.InputFileUpload{Filename: p.Asset.Name, Data: data}
	}
	call, a := providers.Observe(ctx)
	var result *models.Message
	var err error
	switch p.Kind {
	case "photo":
		if file == nil {
			return nil, providers.ErrInvalidRequest
		}
		result, err = c.sdk.SendPhoto(call, &bot.SendPhotoParams{ChatID: p.Target.ChatID, MessageThreadID: int(p.Target.ThreadID), Photo: file, Caption: p.Body.Text, CaptionEntities: p.Body.Entities, ReplyParameters: reply})
	case "video":
		if file == nil {
			return nil, providers.ErrInvalidRequest
		}
		result, err = c.sdk.SendVideo(call, &bot.SendVideoParams{ChatID: p.Target.ChatID, MessageThreadID: int(p.Target.ThreadID), Video: file, Caption: p.Body.Text, CaptionEntities: p.Body.Entities, ReplyParameters: reply})
	case "animation":
		if file == nil {
			return nil, providers.ErrInvalidRequest
		}
		result, err = c.sdk.SendAnimation(call, &bot.SendAnimationParams{ChatID: p.Target.ChatID, MessageThreadID: int(p.Target.ThreadID), Animation: file, Caption: p.Body.Text, CaptionEntities: p.Body.Entities, ReplyParameters: reply})
	case "audio":
		if file == nil {
			return nil, providers.ErrInvalidRequest
		}
		result, err = c.sdk.SendAudio(call, &bot.SendAudioParams{ChatID: p.Target.ChatID, MessageThreadID: int(p.Target.ThreadID), Audio: file, Caption: p.Body.Text, CaptionEntities: p.Body.Entities, ReplyParameters: reply})
	case "voice":
		if file == nil {
			return nil, providers.ErrInvalidRequest
		}
		result, err = c.sdk.SendVoice(call, &bot.SendVoiceParams{ChatID: p.Target.ChatID, MessageThreadID: int(p.Target.ThreadID), Voice: file, Caption: p.Body.Text, CaptionEntities: p.Body.Entities, ReplyParameters: reply})
	case "document":
		if file == nil {
			return nil, providers.ErrInvalidRequest
		}
		result, err = c.sdk.SendDocument(call, &bot.SendDocumentParams{ChatID: p.Target.ChatID, MessageThreadID: int(p.Target.ThreadID), Document: file, Caption: p.Body.Text, CaptionEntities: p.Body.Entities, ReplyParameters: reply})
	case "sticker":
		if file == nil {
			return nil, providers.ErrInvalidRequest
		}
		result, err = c.sdk.SendSticker(call, &bot.SendStickerParams{ChatID: p.Target.ChatID, MessageThreadID: int(p.Target.ThreadID), Sticker: file, ReplyParameters: reply})
	case "location":
		if p.Location == nil {
			return nil, providers.ErrInvalidRequest
		}
		result, err = c.sdk.SendLocation(call, &bot.SendLocationParams{ChatID: p.Target.ChatID, MessageThreadID: int(p.Target.ThreadID), Latitude: p.Location.Latitude, Longitude: p.Location.Longitude, ReplyParameters: reply})
	case "contact":
		if p.Contact == nil {
			return nil, providers.ErrInvalidRequest
		}
		result, err = c.sdk.SendContact(call, &bot.SendContactParams{ChatID: p.Target.ChatID, MessageThreadID: int(p.Target.ThreadID), PhoneNumber: p.Contact.Number, FirstName: p.Contact.Name, VCard: p.Contact.VCard, ReplyParameters: reply})
	default:
		return nil, providers.ErrInvalidRequest
	}
	if err := providers.Outcome("telegram", true, err, a); err != nil {
		return nil, err
	}
	if result == nil || result.ID <= 0 {
		return nil, providers.InvalidResponse("telegram", true)
	}
	return result, nil
}
