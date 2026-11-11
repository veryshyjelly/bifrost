package relay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/attachments"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

type NativeWhatsApp interface {
	SendMedia(context.Context, string, string, sdk.SendMediaRequest, string) (*sdk.MessageResponse, error)
	SendAudio(context.Context, string, sdk.SendAudioRequest, string) (*sdk.MessageResponse, error)
	SendLocation(context.Context, string, sdk.SendLocationRequest, string) (*sdk.MessageResponse, error)
	SendContact(context.Context, string, sdk.SendContactRequest, string) (*sdk.MessageResponse, error)
	SendPoll(context.Context, string, sdk.SendPollRequest, string) (*sdk.MessageResponse, error)
}
type Outbound struct {
	store   OutboundStore
	wa      NativeWhatsApp
	assets  *attachments.Assets
	sources *attachments.Sources
	codec   *attachments.Codec
	replies *control.Service
	c       Options
}

type OutboundStore interface {
	Store
	RefuseOutgoingPacket(context.Context, delivery.Job, string) error
}

func NewOutbound(store OutboundStore, wa NativeWhatsApp, assets *attachments.Assets, sources *attachments.Sources, codec *attachments.Codec, replies *control.Service, c Options) (*Outbound, error) {
	if store == nil || wa == nil || assets == nil || sources == nil || codec == nil || replies == nil || c.SessionID == "" || c.OwnerID <= 0 || c.ManagementChatID >= 0 {
		return nil, delivery.ErrInvalid
	}
	return &Outbound{store: store, wa: wa, assets: assets, sources: sources, codec: codec, replies: replies, c: c}, nil
}
func (s *Outbound) Handle(ctx context.Context, j delivery.Job, e *delivery.Effects, botID string, u *models.Update) error {
	if j.Kind != "tg" || e == nil || botID == "" || u == nil {
		return delivery.ErrInvalid
	}
	m := u.Message
	if m == nil {
		return delivery.ErrUnsupported
	}
	if m.From == nil || m.From.IsBot || m.From.ID != s.c.OwnerID {
		return nil
	}
	if m.Chat.Type != models.ChatTypePrivate && botID != "master" {
		return nil
	}
	if m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != s.c.OwnerID {
		return delivery.ErrInvalid
	}
	if m.Text != "" || m.EditDate != 0 {
		return delivery.ErrUnsupported
	}
	if strings.HasPrefix(strings.TrimSpace(m.Caption), "/") {
		return nil
	}
	key := OutgoingKey(botID, m.Chat.ID, int64(m.ID))
	p, found, err := s.store.RelayPlan(ctx, j.AccountID, key)
	if err != nil {
		return local(err)
	}
	if found && p.Complete {
		return nil
	}
	if found {
		route, exists, err := s.store.ConversationRoute(ctx, j.AccountID, p.WhatsAppChat)
		if err != nil {
			return local(err)
		}
		if exists && (route.Mode == conversation.Muted || route.Mode == conversation.Ignored) {
			return local(s.store.CancelRelay(ctx, j, key))
		}
	} else {
		route, exists, err := s.store.OutgoingRoute(ctx, j, botID, m.Chat.ID, int64(m.MessageThreadID), s.c.ManagementChatID)
		if err != nil {
			return local(err)
		}
		if !exists || route.Mode == conversation.Muted || route.Mode == conversation.Ignored {
			return nil
		}
		if _, exists, err := s.store.TelegramMapping(ctx, j.AccountID, botID, m.Chat.ID, int64(m.ID)); err != nil {
			return local(err)
		} else if exists {
			return nil
		}
		quote := ""
		if m.ReplyToMessage != nil {
			q, found, err := s.store.TelegramMapping(ctx, j.AccountID, botID, m.Chat.ID, int64(m.ReplyToMessage.ID))
			if err != nil {
				return local(err)
			}
			if found {
				if q.WhatsAppChat != route.ChatID || q.ThreadID != route.ThreadID {
					return delivery.ErrInvalid
				}
				if !q.Deleted {
					quote = q.WhatsAppMessage
				}
			}
		}
		request := WhatsAppRequest{ChatID: route.ChatID, Text: m.Caption, QuotedMessageID: quote}
		if s.c.Mentions != nil {
			resolved, err := s.c.Mentions.Outgoing(ctx, j, route.ChatID, m.Caption, m.CaptionEntities)
			if err != nil {
				return err
			}
			request.Text, request.Mentions = resolved.Text, resolved.Mentions
		} else if len(m.CaptionEntities) > 0 || strings.Contains(m.Caption, "@") {
			return delivery.ErrUnsupported
		}
		var original *attachments.Asset
		if _, file := attachments.SelectTelegram(m); file {
			if err := e.Permit(); err != nil {
				return err
			}
			asset, kind, err := s.sources.Telegram(ctx, j.AccountID, botID, m)
			if err != nil {
				return err
			}
			original = &asset
			request.Kind = kind
			upload := asset
			if kind == "sticker" {
				upload, err = s.codec.Sticker(ctx, asset, "whatsapp")
				if err != nil {
					var refusal *providers.Error
					if !errors.As(err, &refusal) || refusal.Status != 415 {
						return err
					}
					upload = asset
					request.Kind = "document"
				}
			}
			if kind == "video_note" || kind == "animation" {
				request.Kind = "video"
			}
			request.Asset = &upload
			request.PTT = kind == "voice"
		} else if m.Location != nil {
			request.Kind = "location"
			request.Location = &sdk.SendLocationRequest{ChatID: route.ChatID, Latitude: m.Location.Latitude, Longitude: m.Location.Longitude, QuotedMessageID: quote}
		} else if m.Contact != nil {
			request.Kind = "contact"
			request.Contact = &sdk.SendContactRequest{ChatID: route.ChatID, ContactName: strings.TrimSpace(m.Contact.FirstName + " " + m.Contact.LastName), ContactNumber: m.Contact.PhoneNumber, QuotedMessageID: quote}
		} else if m.Poll != nil {
			request.Kind = "poll"
			options := make([]string, len(m.Poll.Options))
			for i, option := range m.Poll.Options {
				options[i] = option.Text
			}
			multiple := m.Poll.AllowsMultipleAnswers
			request.Poll = &sdk.SendPollRequest{ChatID: route.ChatID, Name: m.Poll.Question, Options: options, AllowMultipleAnswers: &multiple, QuotedMessageID: quote}
		} else {
			return s.replies.Reply(ctx, j, e, control.Target{BotID: botID, ChatID: m.Chat.ID, ThreadID: int64(m.MessageThreadID)}, presentation.Plain("This Telegram message type cannot be sent to WhatsApp."))
		}
		p = Plan{Key: key, Prefix: key, Direction: "out", WhatsAppChat: route.ChatID, BotID: botID, TelegramChat: m.Chat.ID, TelegramMessage: int64(m.ID), ThreadID: route.ThreadID, Route: route, Outgoing: &request, SourceAsset: original}
		if request.Kind == "sticker" && original != nil {
			fallback := request
			fallback.Kind = "document"
			fallback.Asset = original
			p.OutgoingFallback = &fallback
		}
		p, err = s.store.PrepareRelay(ctx, j, p)
		if err != nil {
			return local(err)
		}
	}
	for {
		effect, request, err := p.FrozenRequest(0)
		if err != nil {
			return err
		}
		_, err = e.Do(effect, request, func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var request WhatsAppRequest
			if json.Unmarshal(raw, &request) != nil {
				return nil, delivery.ErrInvalid
			}
			var result *sdk.MessageResponse
			var err error
			switch request.Kind {
			case "location":
				if request.Location == nil {
					return nil, delivery.ErrInvalid
				}
				result, err = s.wa.SendLocation(ctx, s.c.SessionID, *request.Location, digest(effect))
			case "contact":
				if request.Contact == nil {
					return nil, delivery.ErrInvalid
				}
				result, err = s.wa.SendContact(ctx, s.c.SessionID, *request.Contact, digest(effect))
			case "poll":
				if request.Poll == nil {
					return nil, delivery.ErrInvalid
				}
				result, err = s.wa.SendPoll(ctx, s.c.SessionID, *request.Poll, digest(effect))
			default:
				if request.Asset == nil {
					return nil, delivery.ErrInvalid
				}
				file, openErr := s.assets.Open(ctx, *request.Asset)
				if openErr != nil {
					return nil, local(openErr)
				}
				defer file.Close()
				data, readErr := io.ReadAll(io.LimitReader(file, request.Asset.Size+1))
				if readErr != nil {
					return nil, local(readErr)
				}
				if int64(len(data)) != request.Asset.Size {
					return nil, delivery.ErrLocalRetry
				}
				encoded := base64.StdEncoding.EncodeToString(data)
				if request.Kind == "audio" || request.Kind == "voice" {
					body := sdk.SendAudioRequest{ChatID: request.ChatID, Base64: encoded, Mimetype: request.Asset.MIME, Filename: request.Asset.Name, Caption: request.Text, QuotedMessageID: request.QuotedMessageID, Mentions: request.Mentions}
					if request.PTT {
						ptt := true
						body.PTT = &ptt
					}
					result, err = s.wa.SendAudio(ctx, s.c.SessionID, body, digest(effect))
				} else {
					result, err = s.wa.SendMedia(ctx, s.c.SessionID, request.Kind, sdk.SendMediaRequest{ChatID: request.ChatID, Base64: encoded, Mimetype: request.Asset.MIME, Filename: request.Asset.Name, Caption: request.Text, QuotedMessageID: request.QuotedMessageID, Mentions: request.Mentions}, digest(effect))
				}
			}
			if err != nil {
				return nil, err
			}
			if result == nil || result.MessageID == "" {
				return nil, providers.InvalidResponse("openwa", true)
			}
			return json.Marshal(result)
		})
		if err == nil {
			break
		}
		var refusal *providers.Error
		if p.UseOutgoingFallback || p.OutgoingFallback == nil || !errors.As(err, &refusal) || refusal.Retry || refusal.Uncertain || (refusal.Status != 400 && refusal.Status != 415) {
			return err
		}
		if err := s.store.RefuseOutgoingPacket(ctx, j, p.Key); err != nil {
			return local(err)
		}
		p.UseOutgoingFallback = true
	}
	if err := s.store.RecordRelay(ctx, j, p.Key, 0); err != nil {
		return local(err)
	}
	return local(s.store.CompleteRelay(ctx, j, p.Key))
}
