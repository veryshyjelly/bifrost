package control

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

func local(err error) error {
	if err == nil || errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, delivery.ErrInvalid) || errors.Is(err, delivery.ErrUnsupported) || errors.Is(err, identity.ErrConflict) {
		return err
	}
	return delivery.ErrLocalRetry
}
func (s *Service) Context(ctx context.Context, job delivery.Job, bot string, m *models.Message) (Context, error) {
	if m == nil || m.Chat.ID == 0 || bot == "" {
		return Context{}, delivery.ErrInvalid
	}
	c, err := s.store.ControlContext(ctx, job.AccountID, bot, m, s.management)
	return c, local(err)
}
func (s *Service) Prepared(ctx context.Context, job delivery.Job, key string) (Plan, bool, error) {
	p, ok, err := s.store.ControlPlan(ctx, job.AccountID, key)
	return p, ok, local(err)
}
func (s *Service) Action(ctx context.Context, job delivery.Job, id string) (Action, bool, error) {
	a, ok, err := s.store.ControlAction(ctx, job.AccountID, id)
	return a, ok, local(err)
}

func (s *Service) WasCommand(ctx context.Context, job delivery.Job, bot string, m *models.Message) (bool, error) {
	if m == nil {
		return false, nil
	}
	known, err := s.store.ControlWasCommand(ctx, job.AccountID, bot, m.Chat.ID, int64(m.ID))
	return known, local(err)
}
func (s *Service) Addressed(ctx context.Context, job delivery.Job, botID, username string) (bool, error) {
	if username == "" {
		return true, nil
	}
	c, err := s.clients(ctx, job.AccountID, botID)
	if err != nil {
		return false, err
	}
	me, err := c.Me(ctx)
	if err != nil {
		return false, err
	}
	if me == nil || me.ID <= 0 || me.Username == "" {
		return false, providers.InvalidResponse("telegram", false)
	}
	if botID != "master" && strconv.FormatInt(me.ID, 10) != botID {
		return false, providers.InvalidResponse("telegram", false)
	}
	return strings.EqualFold(me.Username, username), nil
}
func (s *Service) Send(ctx context.Context, job delivery.Job, e *delivery.Effects, p Plan) error {
	if e == nil {
		return delivery.ErrInvalid
	}
	saved, ok, err := s.Prepared(ctx, job, p.Key)
	if err != nil {
		return err
	}
	if ok {
		p = saved
	} else {
		p, err = s.store.PrepareControl(ctx, job, p)
		if err != nil {
			return local(err)
		}
	}
	if p.Complete {
		return nil
	}
	if p.Answer != nil {
		request, err := json.Marshal(p.Answer)
		if err != nil {
			return delivery.ErrInvalid
		}
		key := p.Key + ":answer"
		_, err = e.Do(key, request, func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var a bot.AnswerCallbackQueryParams
			if json.Unmarshal(raw, &a) != nil || a.CallbackQueryID == "" {
				return nil, delivery.ErrInvalid
			}
			client, err := s.clients(ctx, job.AccountID, p.Target.BotID)
			if err != nil {
				return nil, err
			}
			if err := client.AnswerCallback(ctx, &a); err != nil {
				return nil, err
			}
			return json.RawMessage(`{"answered":true}`), nil
		})
		if err != nil && !errors.Is(err, delivery.ErrDiscarded) {
			var refusal *providers.Error
			if !errors.As(err, &refusal) || refusal.Service != "telegram" || refusal.Status != 400 {
				return err
			}
			if err := e.Discard(key); err != nil {
				return err
			}
		}
	}
	for i := range p.Packets {
		key, request, err := p.FrozenPacket(i)
		if err != nil {
			return err
		}
		_, err = e.Do(key, request, func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var frozen struct {
				Target Target
				Packet Packet
			}
			if json.Unmarshal(raw, &frozen) != nil {
				return nil, delivery.ErrInvalid
			}
			client, err := s.clients(ctx, job.AccountID, frozen.Target.BotID)
			if err != nil {
				return nil, err
			}
			params := &bot.SendMessageParams{ChatID: frozen.Target.ChatID, MessageThreadID: int(frozen.Target.ThreadID), Text: frozen.Packet.Text, Entities: frozen.Packet.Entities}
			if frozen.Packet.Markup != nil {
				params.ReplyMarkup = frozen.Packet.Markup
			}
			for _, entity := range params.Entities {
				if entity.Type == models.MessageEntityTypeTextLink {
					params.LinkPreviewOptions = &models.LinkPreviewOptions{IsDisabled: bot.True()}
					break
				}
			}
			r, err := client.SendText(ctx, params)
			if err != nil {
				return nil, err
			}
			if r == nil || r.ID <= 0 || r.Chat.ID != frozen.Target.ChatID || (r.MessageThreadID != 0 && int64(r.MessageThreadID) != frozen.Target.ThreadID && (frozen.Target.ThreadID > 0 || r.IsTopicMessage)) {
				return nil, providers.InvalidResponse("telegram", true)
			}
			return json.Marshal(r)
		})
		if err != nil {
			return err
		}
		if err := s.store.RecordControl(ctx, job, p.Key, i); err != nil {
			return local(err)
		}
	}
	return local(s.store.CompleteControl(ctx, job, p.Key))
}
