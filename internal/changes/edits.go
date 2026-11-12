package changes

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

type Edits struct {
	store    Store
	wa       WhatsApp
	clients  Clients
	mentions Mentions
	c        Settings
}

func NewEdits(store Store, wa WhatsApp, clients Clients, mentions Mentions, c Settings) (*Edits, error) {
	if c.NoticeTTL == 0 {
		c.NoticeTTL = 60
	}
	if store == nil || wa == nil || clients == nil || mentions == nil || c.Session == "" || c.OwnerID <= 0 || c.Management >= 0 || c.NoticeTTL < 5 || c.NoticeTTL > 86400 {
		return nil, delivery.ErrInvalid
	}
	return &Edits{store: store, wa: wa, clients: clients, mentions: mentions, c: c}, nil
}
func (s *Edits) Telegram(ctx context.Context, j delivery.Job, e *delivery.Effects, botID string, u *models.Update) error {
	if j.Kind != "tg" || e == nil || u == nil {
		return delivery.ErrInvalid
	}
	if u.EditedMessage == nil {
		return delivery.ErrUnsupported
	}
	m := u.EditedMessage
	if m.From == nil || m.From.IsBot || m.From.ID != s.c.OwnerID {
		return nil
	}
	if _, command := control.ParseCommand(m); command {
		return nil
	}
	if botID != "master" && m.Chat.Type != models.ChatTypePrivate {
		return nil
	}
	return s.apply(ctx, j, e)
}
func (s *Edits) WhatsApp(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "wa" || e == nil {
		return delivery.ErrInvalid
	}
	var event sdk.WebhookDelivery
	if json.Unmarshal(j.Payload, &event) != nil {
		return delivery.ErrInvalid
	}
	if event.Event != sdk.EventMessageEdited {
		return delivery.ErrUnsupported
	}
	if err := s.store.NormalizeChange(ctx, j, s.c.Session); err != nil {
		return local(err)
	}
	return s.apply(ctx, j, e)
}
func (s *Edits) apply(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	p, found, err := s.store.ChangePlan(ctx, j.AccountID, j.Dedup)
	if err != nil {
		return local(err)
	}
	if !found {
		input, err := s.store.EditInput(ctx, j, s.c)
		if err != nil {
			return local(err)
		}
		p = Plan{Key: j.Dedup, Input: input}
		if input.TelegramMessage != nil && input.ChatID != "" {
			m := input.TelegramMessage
			text, entities := m.Text, m.Entities
			if m.Text == "" {
				text, entities = m.Caption, m.CaptionEntities
			}
			resolved, err := s.mentions.Outgoing(ctx, j, input.ChatID, text, entities)
			if err != nil {
				return err
			}
			notice := "This edit was not applied on WhatsApp. WhatsApp may refuse edits to older or unsupported messages. New messages can continue."
			if resolved.Text == "" {
				notice = "Clearing this WhatsApp caption was refused by the provider. Its WhatsApp caption was kept; new messages can continue."
			}
			p.Steps = append(p.Steps, Step{Kind: "whatsapp.edit", Edit: &sdk.EditMessageRequest{ChatID: input.ChatID, MessageID: input.MessageID, Body: resolved.Text, Mentions: resolved.Mentions}, Refusal: &control.Notice{Target: control.Target{BotID: input.TelegramBot, ChatID: m.Chat.ID, ThreadID: int64(m.MessageThreadID)}, Body: presentation.Plain(notice), ReplyTo: int64(m.ID), TTLSeconds: s.c.NoticeTTL}, State: "pending"})
		} else {
			for _, destination := range input.Destinations {
				body, err := s.mentions.Incoming(ctx, j, input.ChatID, input.Body, input.MentionedIDs, destination.Target.ChatID)
				if err != nil {
					return err
				}
				text, err := presentation.Relay(destination.Format, body)
				if err != nil {
					return err
				}
				limit, kind := 3800, "telegram.text"
				if destination.Caption {
					limit, kind = 1000, "telegram.caption"
				}
				parts, err := presentation.Split(text, limit)
				if err != nil || len(parts) == 0 {
					return delivery.ErrInvalid
				}
				part := parts[0]
				if !destination.Caption && part.Text == "" {
					part = presentation.Plain("[Empty message]")
				}
				fallback := destination.Target
				if fallback.ChatID < 0 {
					fallback.BotID = "master"
				}
				format := destination.Format
				format.Bot = fallback.BotID
				format.EditedNotice = true
				notice, err := presentation.Relay(format, body)
				if err != nil {
					return err
				}
				p.Steps = append(p.Steps, Step{Kind: kind, Destination: destination, Body: part, State: "pending", Refusal: &control.Notice{Target: fallback, Body: notice, ReplyTo: destination.MessageID, TTLSeconds: s.c.NoticeTTL}})
			}
		}
		p, err = s.store.PrepareChange(ctx, j, p, s.c)
		if err != nil {
			return local(err)
		}
	}
	if p.Complete {
		return nil
	}
	for index, step := range p.Steps {
		if step.State == "done" || step.State == "refused" {
			continue
		}
		key, request, err := p.Request(index)
		if err != nil {
			return err
		}
		_, err = e.Do(key, request, func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var step Step
			if json.Unmarshal(raw, &step) != nil {
				return nil, delivery.ErrInvalid
			}
			if step.Kind == "whatsapp.edit" {
				if step.Edit == nil {
					return nil, delivery.ErrInvalid
				}
				result, err := s.wa.Edit(ctx, s.c.Session, *step.Edit)
				if err != nil {
					return nil, err
				}
				if result == nil || result.MessageID != step.Edit.MessageID {
					return nil, providers.InvalidResponse("openwa", true)
				}
				return json.Marshal(result)
			}
			client, err := s.clients(ctx, j.AccountID, step.Destination.Target.BotID)
			if err != nil {
				return nil, local(err)
			}
			switch step.Kind {
			case "telegram.text":
				err = client.EditText(ctx, &bot.EditMessageTextParams{ChatID: step.Destination.Target.ChatID, MessageID: int(step.Destination.MessageID), Text: step.Body.Text, Entities: step.Body.Entities, LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: bot.True()}})
			case "telegram.caption":
				err = client.EditCaption(ctx, &bot.EditMessageCaptionParams{ChatID: step.Destination.Target.ChatID, MessageID: int(step.Destination.MessageID), Caption: step.Body.Text, CaptionEntities: step.Body.Entities})
			default:
				return nil, delivery.ErrInvalid
			}
			if err != nil {
				return nil, err
			}
			return json.RawMessage(`{"success":true}`), nil
		})
		if err != nil {
			var refusal *providers.Error
			if !errors.As(err, &refusal) || refusal.Retry || refusal.Uncertain {
				return err
			}
			if step.Kind == "whatsapp.edit" {
				switch refusal.Status {
				case 400, 403, 404, 413, 422:
				default:
					return err
				}
			} else if refusal.Status != 400 {
				return err
			}
			if err := s.store.RefuseChange(ctx, j, p.Key, index, refusal.Status, refusal.Reason); err != nil {
				return local(err)
			}
			continue
		}
		if err := s.store.RecordChange(ctx, j, p.Key, index); err != nil {
			return local(err)
		}
	}
	return local(s.store.CompleteChange(ctx, j, p.Key))
}
