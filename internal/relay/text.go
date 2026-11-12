package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

type Options struct {
	SessionID                 string
	OwnerID, ManagementChatID int64
	Mentions                  Mentions
}
type Service struct {
	store    Store
	routing  Routing
	telegram TelegramClients
	wa       WhatsApp
	c        Options
}

func New(store Store, routing Routing, telegram TelegramClients, wa WhatsApp, c Options) (*Service, error) {
	if store == nil || routing == nil || telegram == nil || wa == nil || c.SessionID == "" || c.OwnerID <= 0 || c.ManagementChatID >= 0 {
		return nil, delivery.ErrInvalid
	}
	return &Service{store: store, routing: routing, telegram: telegram, wa: wa, c: c}, nil
}
func digest(text string) string { h := sha256.Sum256([]byte(text)); return hex.EncodeToString(h[:]) }
func IncomingKey(chat, id string) string {
	b, _ := json.Marshal([]string{chat, id})
	return "wa-text:" + digest(string(b))
}
func OutgoingKey(bot string, chat, message int64) string {
	prefix := ""
	if chat > 0 {
		prefix = bot + ":"
	}
	return "send:" + prefix + strconv.FormatInt(chat, 10) + ":" + strconv.FormatInt(message, 10)
}
func (p Plan) PartKey(index int) string { return p.Prefix + ":text:" + strconv.Itoa(index) }
func local(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, delivery.ErrInvalid) || errors.Is(err, delivery.ErrUncertain) || errors.Is(err, delivery.ErrUnsupported) || errors.Is(err, identity.ErrConflict) {
		return err
	}
	return delivery.ErrLocalRetry
}

func (s *Service) IncomingText(ctx context.Context, job delivery.Job, e *delivery.Effects, m Incoming) error {
	if e == nil || job.Kind != "wa" || m.ChatID == "" || m.MessageID == "" || job.Stream != "wa:"+m.ChatID || !utf8.ValidString(m.Body) {
		return delivery.ErrInvalid
	}
	if revoked, err := s.store.MessageRevoked(ctx, job.AccountID, m.ChatID, m.MessageID); err != nil {
		return local(err)
	} else if revoked {
		return nil
	}
	echo, err := s.store.RelayEcho(ctx, job.AccountID, m.ChatID, m.MessageID)
	if err != nil {
		return local(err)
	}
	if echo.Recognized {
		return nil
	}
	if m.Own && echo.Pending {
		return delivery.ErrLocalRetry
	}
	key := IncomingKey(m.ChatID, m.MessageID)
	plan, found, err := s.store.RelayPlan(ctx, job.AccountID, key)
	if err != nil {
		return local(err)
	}
	if found && plan.Complete {
		return nil
	}
	if !found {
		if complete, err := s.store.MessageComplete(ctx, job.AccountID, m.ChatID, m.MessageID); err != nil {
			return local(err)
		} else if complete {
			return nil
		}
	}
	route, known, err := s.store.ConversationRoute(ctx, job.AccountID, m.ChatID)
	if err != nil {
		return local(err)
	}
	if known && (route.Mode == conversation.Muted || route.Mode == conversation.Ignored) {
		if found {
			return local(s.store.CancelRelay(ctx, job, key))
		}
		return nil
	}
	if !found {
		if len(m.MentionedIDs) > 0 && s.c.Mentions == nil {
			return delivery.ErrUnsupported
		}
		name := m.ConversationName
		if name == "" {
			name = m.Name
		}
		if name == "" {
			name = m.ChatID
		}
		route, err = s.routing.EnsureInbox(ctx, job, e, m.ChatID, name)
		if err != nil {
			return err
		}
		target, err := s.routing.SelectTarget(ctx, job.AccountID, route, m.SenderID, conversation.Flags{FromMe: m.Own})
		if err != nil {
			return err
		}
		if target.Suppressed {
			return nil
		}
		quote := int64(0)
		if m.QuotedMessageID != "" {
			q, exists, err := s.store.WhatsAppQuote(ctx, job.AccountID, m.ChatID, m.QuotedMessageID, target.BotID, target.ChatID, target.ThreadID)
			if err != nil {
				return local(err)
			}
			if exists {
				quote = q.TelegramMessage
			}
		}
		link := m.LinkSenderID
		if link == "" && !m.Own {
			link = m.SenderID
		}
		displayName := m.Name
		if displayName == "" {
			displayName = m.SenderID
		}
		if displayName == "" {
			displayName = m.ChatID
		}
		content := presentation.Plain(m.Body)
		if s.c.Mentions != nil && len(m.MentionedIDs) > 0 {
			content, err = s.c.Mentions.Incoming(ctx, job, m.ChatID, m.Body, m.MentionedIDs, target.ChatID)
			if err != nil {
				return err
			}
		}
		format := presentation.Options{Bot: target.BotID, Sender: link, Name: displayName, Own: m.Own}
		body, err := presentation.Relay(format, content)
		if err != nil {
			return delivery.ErrInvalid
		}
		parts, err := presentation.Split(body, 3800)
		if err != nil {
			return delivery.ErrInvalid
		}
		plan = Plan{Key: key, Direction: "in", WhatsAppChat: m.ChatID, WhatsAppMessage: m.MessageID, BotID: target.BotID, TelegramChat: target.ChatID, ThreadID: target.ThreadID, Prefix: job.Dedup + ":to:" + target.BotID + ":" + strconv.FormatInt(target.ChatID, 10), Route: route}
		plan.Presentation = &format
		for _, p := range parts {
			text := p.Text
			if text == "" {
				text = "[Empty message]"
			}
			plan.Parts = append(plan.Parts, TelegramRequest{BotID: target.BotID, ChatID: target.ChatID, ThreadID: target.ThreadID, Text: text, Entities: p.Entities, ReplyTo: quote})
		}
		plan, err = s.store.PrepareRelay(ctx, job, plan)
		if err != nil {
			return local(err)
		}
	}
	return s.deliverIncoming(ctx, job, e, plan)
}

// ResumeIncoming continues an adopted immutable plan using its original effect keys.
func (s *Service) ResumeIncoming(ctx context.Context, job delivery.Job, e *delivery.Effects, key string) error {
	plan, found, err := s.store.RelayPlan(ctx, job.AccountID, key)
	if err != nil {
		return local(err)
	}
	if !found || plan.Direction != "in" {
		return delivery.ErrInvalid
	}
	if plan.Complete {
		return nil
	}
	return s.deliverIncoming(ctx, job, e, plan)
}
func (s *Service) deliverIncoming(ctx context.Context, job delivery.Job, e *delivery.Effects, plan Plan) error {
	for i := range plan.Parts {
		key, request, err := plan.FrozenRequest(i)
		if err != nil {
			return err
		}
		_, err = e.Do(key, request, func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var p TelegramRequest
			if json.Unmarshal(raw, &p) != nil {
				return nil, delivery.ErrInvalid
			}
			client, err := s.telegram(ctx, job.AccountID, p.BotID)
			if err != nil {
				return nil, err
			}
			params := &bot.SendMessageParams{ChatID: p.ChatID, MessageThreadID: int(p.ThreadID), Text: p.Text, Entities: p.Entities}
			for _, entity := range p.Entities {
				if entity.Type == models.MessageEntityTypeTextLink {
					params.LinkPreviewOptions = &models.LinkPreviewOptions{IsDisabled: bot.True()}
					break
				}
			}
			if p.ReplyTo > 0 {
				params.ReplyParameters = &models.ReplyParameters{MessageID: int(p.ReplyTo), AllowSendingWithoutReply: true}
			}
			result, err := client.SendText(ctx, params)
			if err != nil {
				return nil, err
			}
			if result == nil || result.ID <= 0 || result.Chat.ID != p.ChatID || (result.MessageThreadID != 0 && int64(result.MessageThreadID) != p.ThreadID && (p.ThreadID > 0 || result.IsTopicMessage)) {
				return nil, providers.InvalidResponse("telegram", true)
			}
			return json.Marshal(result)
		})
		if err != nil {
			return err
		}
		if err := s.store.RecordRelay(ctx, job, plan.Key, i); err != nil {
			return local(err)
		}
	}
	return local(s.store.CompleteRelay(ctx, job, plan.Key))
}

func ordinaryText(m *models.Message) bool {
	if m.Text == "" || !utf8.ValidString(m.Text) {
		return false
	}
	if strings.HasPrefix(strings.TrimFunc(m.Text, func(r rune) bool { return r == 0xfeff || (r != 0x85 && unicode.IsSpace(r)) }), "/") {
		return false
	}
	return true
}
func plainOutgoing(m *models.Message) bool {
	if !ordinaryText(m) {
		return false
	}
	for _, entity := range m.Entities {
		if entity.Type == models.MessageEntityTypeTextMention || entity.Type == models.MessageEntityTypeMention {
			return false
		}
	}
	// Mention tags and usernames need the verified mention/roster capability.
	for i, r := range m.Text {
		if r != '@' || i+1 == len(m.Text) {
			continue
		}
		if i == 0 {
			return false
		}
		prev, _ := utf8.DecodeLastRuneInString(m.Text[:i])
		if !unicode.IsLetter(prev) && !unicode.IsNumber(prev) && prev != '_' && prev != '@' && prev != '/' {
			return false
		}
	}
	return true
}

func (s *Service) OutgoingText(ctx context.Context, job delivery.Job, e *delivery.Effects, input Outgoing) error {
	m := input.Message
	if e == nil || job.Kind != "tg" || m == nil || m.ID <= 0 || input.BotID == "" {
		return delivery.ErrInvalid
	}
	var source struct {
		Bot string `json:"bot"`
	}
	if json.Unmarshal(job.Payload, &source) != nil || source.Bot != input.BotID {
		return delivery.ErrInvalid
	}
	if m.From == nil || m.From.IsBot || m.From.ID != s.c.OwnerID {
		return nil
	}
	if input.Edited || m.EditDate != 0 || !ordinaryText(m) {
		return delivery.ErrUnsupported
	}
	if m.Chat.Type != models.ChatTypePrivate && input.BotID != "master" {
		return nil
	}
	if m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != s.c.OwnerID {
		return delivery.ErrInvalid
	}
	key := OutgoingKey(input.BotID, m.Chat.ID, int64(m.ID))
	plan, found, err := s.store.RelayPlan(ctx, job.AccountID, key)
	if err != nil {
		return local(err)
	}
	if found && plan.Complete {
		return nil
	}
	if found {
		route, exists, err := s.store.ConversationRoute(ctx, job.AccountID, plan.WhatsAppChat)
		if err != nil {
			return local(err)
		}
		if exists && (route.Mode == conversation.Muted || route.Mode == conversation.Ignored) {
			return local(s.store.CancelRelay(ctx, job, key))
		}
	}
	if !found {
		if s.c.Mentions == nil && !plainOutgoing(m) {
			return delivery.ErrUnsupported
		}
		route, exists, err := s.store.OutgoingRoute(ctx, job, input.BotID, m.Chat.ID, int64(m.MessageThreadID), s.c.ManagementChatID)
		if err != nil {
			return local(err)
		}
		if !exists {
			return nil
		}
		if route.Mode == conversation.Muted || route.Mode == conversation.Ignored {
			return nil
		}
		mapped, exists, err := s.store.TelegramMapping(ctx, job.AccountID, input.BotID, m.Chat.ID, int64(m.ID))
		if err != nil {
			return local(err)
		}
		if exists {
			if mapped.Direction == "out" {
				return nil
			}
			return nil
		}
		quote := ""
		if m.ReplyToMessage != nil {
			q, exists, err := s.store.TelegramMapping(ctx, job.AccountID, input.BotID, m.Chat.ID, int64(m.ReplyToMessage.ID))
			if err != nil {
				return local(err)
			}
			if exists {
				if q.WhatsAppChat != route.ChatID || q.ThreadID != route.ThreadID {
					return delivery.ErrInvalid
				}
				if !q.Deleted {
					quote = q.WhatsAppMessage
				}
			}
		}
		out := WhatsAppRequest{ChatID: route.ChatID, Text: m.Text, QuotedMessageID: quote}
		if s.c.Mentions != nil {
			resolved, err := s.c.Mentions.Outgoing(ctx, job, route.ChatID, m.Text, m.Entities)
			if err != nil {
				return err
			}
			out.Text = resolved.Text
			out.Mentions = resolved.Mentions
		}
		plan = Plan{Key: key, Prefix: key, Direction: "out", WhatsAppChat: route.ChatID, BotID: input.BotID, TelegramChat: m.Chat.ID, TelegramMessage: int64(m.ID), ThreadID: route.ThreadID, Route: route, Outgoing: &out}
		plan, err = s.store.PrepareRelay(ctx, job, plan)
		if err != nil {
			return local(err)
		}
	}
	effect, request, err := plan.FrozenRequest(0)
	if err != nil {
		return err
	}
	_, err = e.Do(effect, request, func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
		var p WhatsAppRequest
		if json.Unmarshal(raw, &p) != nil {
			return nil, delivery.ErrInvalid
		}
		r, err := s.wa.SendText(ctx, s.c.SessionID, sdk.SendTextRequest{ChatID: p.ChatID, Text: p.Text, QuotedMessageID: p.QuotedMessageID, Mentions: p.Mentions}, digest(plan.Prefix))
		if err != nil {
			return nil, err
		}
		if r == nil || r.MessageID == "" {
			return nil, providers.InvalidResponse("openwa", true)
		}
		return json.Marshal(r)
	})
	if err != nil {
		return err
	}
	if err := s.store.RecordRelay(ctx, job, plan.Key, 0); err != nil {
		return local(err)
	}
	return local(s.store.CompleteRelay(ctx, job, plan.Key))
}

// MarkRead handles only the independently queued receipt operation. Its failure
// cannot replay the owner message whose mapping and send receipt already exist.
func (s *Service) MarkRead(ctx context.Context, job delivery.Job, e *delivery.Effects) error {
	var event struct {
		Type string `json:"type"`
		Data struct {
			ChatID string `json:"chatId"`
		} `json:"data"`
	}
	if job.Kind != "management" || json.Unmarshal(job.Payload, &event) != nil || event.Type != "mark_read" || event.Data.ChatID == "" {
		return delivery.ErrUnsupported
	}
	request, _ := json.Marshal(event.Data)
	_, err := e.Do(job.Dedup+":read", request, func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
		var p struct {
			ChatID string `json:"chatId"`
		}
		if json.Unmarshal(raw, &p) != nil || p.ChatID == "" {
			return nil, delivery.ErrInvalid
		}
		if err := s.wa.MarkRead(ctx, s.c.SessionID, p.ChatID); err != nil {
			return nil, err
		}
		return json.RawMessage(`{"read":true}`), nil
	})
	return err
}
