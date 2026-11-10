package ingress

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

func object(raw json.RawMessage) map[string]json.RawMessage {
	var out map[string]json.RawMessage
	_ = json.Unmarshal(raw, &out)
	return out
}
func text(raw json.RawMessage) string { var out string; _ = json.Unmarshal(raw, &out); return out }
func integer(raw json.RawMessage) (int64, bool) {
	// JSON numbers such as 1.0 and 1e2 are safe integers in the baseline JS runtime.
	var n float64
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &n) != nil || n < -9007199254740991 || n > 9007199254740991 || float64(int64(n)) != n {
		return 0, false
	}
	return int64(n), true
}

func (s *Service) openwa(ctx context.Context, raw []byte, body map[string]json.RawMessage) (bool, error) {
	var hook sdk.WebhookDelivery
	if json.Unmarshal(raw, &hook) != nil {
		return false, invalid()
	}
	switch text(body["event"]) {
	case sdk.EventMessageReceived, sdk.EventMessageSent, sdk.EventMessageEdited, sdk.EventMessageRevoked, sdk.EventMessageReaction, sdk.EventPollVote, sdk.EventStatusReceived, sdk.EventSessionStatus, sdk.EventSessionDisconnected, sdk.EventMessageFailed:
	default:
		return false, invalid()
	}
	data := object(body["data"])
	if text(body["sessionId"]) != s.c.SessionID || text(body["idempotencyKey"]) == "" || data == nil {
		return false, invalid()
	}
	chat := text(data["chatId"])
	if hook.Event == sdk.EventStatusReceived {
		chat = "status@broadcast"
	} else if chat == "" {
		chat = "session"
	}
	body["accountId"], _ = json.Marshal(s.c.AccountID)
	payload, _ := json.Marshal(body)
	return s.s.Admit(ctx, &delivery.Event{AccountID: s.c.AccountID, Dedup: "ow:" + hook.IdempotencyKey, Stream: "wa:" + chat, Kind: "wa", Payload: payload}, nil)
}

var command = regexp.MustCompile(`^/([a-zA-Z0-9_]+)(?:@([a-zA-Z0-9_]+))?(?:[\s\v\p{Zs}\p{Zl}\p{Zp}\x{feff}]|$)`)
var serviceKey = regexp.MustCompile(`^(new_chat_participant|new_chat_member|new_chat_members|left_chat_participant|left_chat_member|new_chat_title|new_chat_photo|delete_chat_photo|group_chat_created|supergroup_chat_created|channel_chat_created|pinned_message|migrate_to_chat_id|migrate_from_chat_id|message_auto_delete_timer_changed|forum_topic_|general_forum_topic_|video_chat_|write_access_allowed|users_shared|chat_shared|boost_added|chat_background_set|community_chat_joined)`)

func commandParts(m *models.Message) []string {
	value := m.Text
	if value == "" {
		value = m.Caption
	}
	return command.FindStringSubmatch(strings.TrimFunc(value, func(r rune) bool { return r == 0xfeff || (r != 0x85 && unicode.IsSpace(r)) }))
}

func actor(u *models.Update) *models.User {
	switch {
	case u.CallbackQuery != nil:
		return &u.CallbackQuery.From
	case u.Message != nil:
		return u.Message.From
	case u.EditedMessage != nil:
		return u.EditedMessage.From
	case u.MessageReaction != nil:
		return u.MessageReaction.User
	case u.MyChatMember != nil:
		return &u.MyChatMember.From
	case u.ManagedBot != nil:
		return u.ManagedBot.User
	case u.PollAnswer != nil:
		return u.PollAnswer.User
	default:
		return nil
	}
}

func (s *Service) telegram(ctx context.Context, bot string, raw []byte, body map[string]json.RawMessage) (bool, error) {
	registered := Bot{ID: bot}
	if bot != "master" {
		var found bool
		var err error
		registered, found, err = s.s.IngressBot(ctx, s.c.AccountID, bot)
		if err != nil {
			return false, err
		}
		if !found {
			return false, &refusal{404, "Unknown bot"}
		}
	}
	// Use the SDK model for native update types, while retaining the raw update
	// (including fields added by future Telegram versions) as the durable payload.
	var update models.Update
	id, validID := integer(body["update_id"])
	modelBody := make(map[string]json.RawMessage, len(body))
	for k, v := range body {
		modelBody[k] = v
	}
	delete(modelBody, "update_id") // SDK's int64 decoder rejects JS-safe exponent notation.
	modelRaw, _ := json.Marshal(modelBody)
	if json.Unmarshal(modelRaw, &update) != nil {
		return false, invalid()
	}
	m := update.Message
	observedRaw := body["message"]
	if m == nil {
		m = update.EditedMessage
		observedRaw = body["edited_message"]
	}
	observed := m
	if m == nil && update.CallbackQuery != nil {
		m = update.CallbackQuery.Message.Message
		if m == nil && update.CallbackQuery.Message.InaccessibleMessage != nil {
			im := update.CallbackQuery.Message.InaccessibleMessage
			m = &models.Message{ID: im.MessageID, Chat: im.Chat}
		}
	}
	thread, known := int64(0), false
	if m != nil && m.Chat.Type != models.ChatTypePrivate {
		route, found, err := s.s.IngressRoute(ctx, s.c.AccountID, m.Chat.ID, int64(m.MessageThreadID), s.c.ManagementChatID)
		if err != nil {
			return false, err
		}
		known = found
		if found {
			thread = route.ThreadID
		} else if m.IsTopicMessage {
			thread = int64(m.MessageThreadID)
		}
	}
	var observation *Observation
	if observed != nil && observed.ID > 0 && (observed.Chat.ID == s.c.OwnerID || (observed.Chat.Type != models.ChatTypePrivate && (observed.Chat.ID == s.c.ManagementChatID || known))) {
		kind := "chat"
		fields := object(observedRaw)
		switch {
		case observed.SupergroupChatCreated || observed.ChannelChatCreated || observed.ForumTopicCreated != nil:
			kind = "protected"
		default:
			for k := range fields {
				if serviceKey.MatchString(k) {
					kind = "service"
					break
				}
			}
			if kind == "chat" && observed.From != nil && observed.From.IsBot {
				sender := strconv.FormatInt(observed.From.ID, 10)
				_, own, err := s.s.IngressBot(ctx, s.c.AccountID, sender)
				if err != nil {
					return false, err
				}
				if own || observed.From.ID == s.c.MasterBotID {
					kind = "log"
				}
			}
			if kind == "chat" && commandParts(observed) != nil {
				kind = "command"
			}
		}
		scope := bot
		if observed.Chat.ID < 0 {
			scope = "*"
		}
		date := int64(observed.Date) * 1000
		if date == 0 {
			date = s.c.Now().UnixMilli()
		}
		observation = &Observation{AccountID: s.c.AccountID, Scope: scope, Bot: bot, Kind: kind, ChatID: observed.Chat.ID, MessageID: int64(observed.ID), ThreadID: thread, Date: date, Payload: observedRaw}
	}
	owner := actor(&update)
	// encoding/json accepts case-insensitive field aliases; actor identity must
	// agree with the exact lower-case keys in the authenticated Telegram schema.
	var sourceActor map[string]json.RawMessage
	for _, source := range []struct{ update, key string }{{"callback_query", "from"}, {"message", "from"}, {"edited_message", "from"}, {"message_reaction", "user"}, {"my_chat_member", "from"}, {"managed_bot", "user"}, {"poll_answer", "user"}} {
		if event := object(body[source.update]); event != nil {
			sourceActor = object(event[source.key])
			break
		}
	}
	sourceID, sourceOK := integer(sourceActor["id"])
	var sourceBot bool
	_ = json.Unmarshal(sourceActor["is_bot"], &sourceBot)
	if owner != nil && (!sourceOK || sourceID != owner.ID || sourceBot) {
		owner = nil
	}
	if owner == nil || owner.IsBot || owner.ID != s.c.OwnerID {
		return s.s.Admit(ctx, nil, observation)
	}
	if !validID {
		return false, invalid()
	}
	private := m != nil && m.Chat.Type == models.ChatTypePrivate
	if m == nil && update.MessageReaction != nil {
		private = update.MessageReaction.Chat.Type == models.ChatTypePrivate
	}
	directed := false
	if m != nil {
		parts := commandParts(m)
		directed = parts != nil && parts[2] != "" && strings.EqualFold(parts[2], registered.Username)
	}
	if !private && bot != "master" && update.MyChatMember == nil && update.CallbackQuery == nil && !directed {
		return s.s.Admit(ctx, nil, observation)
	}
	stream := "tg:" + bot
	if m != nil {
		prefix := ""
		if private {
			prefix = bot + ":"
		}
		stream = "tg:" + prefix + strconv.FormatInt(m.Chat.ID, 10) + ":" + strconv.FormatInt(thread, 10)
	} else if update.MessageReaction != nil {
		stream = "tg-reaction:" + bot + ":" + strconv.FormatInt(update.MessageReaction.Chat.ID, 10)
	}
	payload, _ := json.Marshal(struct {
		Bot    string          `json:"bot"`
		Update json.RawMessage `json:"update"`
	}{bot, raw})
	return s.s.Admit(ctx, &delivery.Event{AccountID: s.c.AccountID, Dedup: "tg:" + bot + ":" + strconv.FormatInt(id, 10), Stream: stream, Kind: "tg", Payload: payload}, observation)
}
