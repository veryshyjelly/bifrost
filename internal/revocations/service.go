package revocations

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func (s *Service) Incoming(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "wa" || e == nil {
		return delivery.ErrInvalid
	}
	var hook sdk.WebhookDelivery
	if json.Unmarshal(j.Payload, &hook) != nil {
		return delivery.ErrInvalid
	}
	if hook.Event != sdk.EventMessageRevoked {
		return delivery.ErrUnsupported
	}
	if err := s.store.NormalizeChange(ctx, j, s.c.Session); err != nil {
		return local(err)
	}
	return local(s.store.AdmitRevocation(ctx, j, s.c))
}
func (s *Service) Callback(ctx context.Context, j delivery.Job, e *delivery.Effects, _ string, u *models.Update) error {
	if j.Kind != "tg" || u == nil || e == nil {
		return delivery.ErrInvalid
	}
	if u.CallbackQuery == nil || !strings.HasPrefix(u.CallbackQuery.Data, "rev-delete:") && !strings.HasPrefix(u.CallbackQuery.Data, "rev-keep:") {
		return delivery.ErrUnsupported
	}
	if u.CallbackQuery.From.IsBot || u.CallbackQuery.From.ID != s.c.OwnerID {
		return nil
	}
	p, err := s.store.PrepareRevocationCallback(ctx, j, s.c)
	if err != nil {
		return local(err)
	}
	return s.replies.Send(ctx, j, e, p)
}
func refusal(err error) bool {
	var p *providers.Error
	return errors.As(err, &p) && !p.Retry && !p.Uncertain && (p.Status == 400 || p.Status == 403)
}

func (s *Service) Process(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "control-revoke" || e == nil {
		return delivery.ErrInvalid
	}
	task, active, err := s.store.RevocationStep(ctx, j, s.c)
	if err != nil {
		return local(err)
	}
	if !active {
		return nil
	}
	var job Job
	if json.Unmarshal(j.Payload, &job) != nil {
		return delivery.ErrInvalid
	}
	switch job.Stage {
	case "present":
		if task.Mode == "inline" {
			if err := e.Permit(); err != nil {
				return err
			}
			client, err := s.clients(ctx, j.AccountID, task.Primary.Target.BotID)
			if err != nil {
				return local(err)
			}
			err = client.EditMarkup(ctx, &bot.EditMessageReplyMarkupParams{ChatID: task.Primary.Target.ChatID, MessageID: int(task.Primary.MessageID), ReplyMarkup: task.Markup()})
			if err == nil {
				return local(s.store.MarkRevocationPresented(ctx, j))
			}
			if !refusal(err) {
				return err
			}
			if err := s.store.UseRevocationReply(ctx, j); err != nil {
				return local(err)
			}
		}
		p, err := s.store.RevocationPrompt(ctx, j)
		if err != nil {
			return local(err)
		}
		if err := s.replies.Send(ctx, j, e, p); err != nil {
			var p *providers.Error
			if !errors.As(err, &p) || p.Retry || p.Uncertain || p.Status != 400 {
				return err
			}
			return local(s.store.RefuseRevocationPrompt(ctx, j))
		}
		return local(s.store.MarkRevocationPresented(ctx, j))
	case "expire":
		if task.ExpiresAt > time.Now().UnixMilli() {
			return control.WaitUntil{Deadline: time.UnixMilli(task.ExpiresAt)}
		}
		accepted, err := s.store.BeginRevocationExpiry(ctx, j)
		if err != nil {
			return local(err)
		}
		if !accepted {
			return nil
		}
		return s.retire(ctx, j, e, task)
	case "keep":
		return s.retire(ctx, j, e, task)
	case "delete":
		if task.Cursor >= len(task.Copies) {
			return s.retire(ctx, j, e, task)
		}
		copy := task.Copies[task.Cursor]
		id := copy.Target.BotID
		if copy.Target.ChatID < 0 {
			id = "master"
		}
		if err := e.Permit(); err != nil {
			return err
		}
		client, err := s.clients(ctx, j.AccountID, id)
		if err != nil {
			return local(err)
		}
		err = client.DeleteMessages(ctx, &bot.DeleteMessagesParams{ChatID: copy.Target.ChatID, MessageIDs: []int{int(copy.MessageID)}})
		if err != nil && !refusal(err) {
			return err
		}
		return local(s.store.AdvanceRevocationDelete(ctx, j, err == nil))
	default:
		return delivery.ErrInvalid
	}
}
func (s *Service) retire(ctx context.Context, j delivery.Job, e *delivery.Effects, t Task) error {
	removed := false
	if t.Mode == "reply" && t.PromptMessageID > 0 {
		if err := e.Permit(); err != nil {
			return err
		}
		client, err := s.clients(ctx, j.AccountID, t.NoticeBot)
		if err != nil {
			return local(err)
		}
		err = client.DeleteMessages(ctx, &bot.DeleteMessagesParams{ChatID: t.Primary.Target.ChatID, MessageIDs: []int{int(t.PromptMessageID)}})
		if err != nil && !refusal(err) {
			return err
		}
		removed = err == nil
	} else if t.Presented {
		if err := e.Permit(); err != nil {
			return err
		}
		client, err := s.clients(ctx, j.AccountID, t.Primary.Target.BotID)
		if err != nil {
			return local(err)
		}
		if err := client.EditMarkup(ctx, &bot.EditMessageReplyMarkupParams{ChatID: t.Primary.Target.ChatID, MessageID: int(t.Primary.MessageID), ReplyMarkup: &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}}}); err != nil && !refusal(err) {
			return err
		}
	}
	return local(s.store.FinishRevocation(ctx, j, removed))
}
