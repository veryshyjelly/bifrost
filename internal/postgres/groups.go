package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func groupToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure randomness unavailable")
	}
	return hex.EncodeToString(b[:])
}
func validGroupSettings(c control.GroupSettings) bool {
	return c.OwnerID > 0 && c.Management < 0 && c.CommandTTL >= 5 && c.CommandTTL <= 86400 && c.ControlTTL >= 5 && c.ControlTTL <= 86400
}
func saveGroupPlan(ctx context.Context, q *db.Queries, row db.Job, p control.Plan) error {
	b, _ := json.Marshal(p)
	if err := q.PrepareControl(ctx, db.PrepareControlParams{AccountID: row.AccountID, PlanKey: p.Key, JobID: row.ID, Payload: b}); err != nil {
		return err
	}
	if c := p.Command; c != nil {
		date := c.Date
		if date == 0 {
			date = time.Now().UnixMilli()
		}
		if err := q.ObserveControl(ctx, db.ObserveControlParams{AccountID: row.AccountID, Scope: relayScope(p.Target.BotID, p.Target.ChatID), BotID: p.Target.BotID, ChatID: p.Target.ChatID, MessageID: c.MessageID, ThreadID: p.Target.ThreadID, Kind: "command", MessageDate: date, Payload: row.Payload, ExpiresAt: time.Now().Add(time.Duration(c.TTLSeconds) * time.Second).UnixMilli()}); err != nil {
			return err
		}
		return q.ScheduleControlExpiry(ctx, db.ScheduleControlExpiryParams{AccountID: row.AccountID, Scope: relayScope(p.Target.BotID, p.Target.ChatID), ChatID: p.Target.ChatID, MessageID: c.MessageID})
	}
	return nil
}
func existingGroupPlan(ctx context.Context, q *db.Queries, row db.Job) (control.Plan, bool, error) {
	r, err := q.ControlPlan(ctx, db.ControlPlanParams{AccountID: row.AccountID, PlanKey: row.Dedup})
	if errors.Is(err, pgx.ErrNoRows) {
		return control.Plan{}, false, nil
	}
	if err != nil {
		return control.Plan{}, false, err
	}
	if r.JobID != row.ID {
		return control.Plan{}, false, delivery.ErrInvalid
	}
	p, err := decodeControl(r)
	return p, true, err
}
func createMirrorChoice(ctx context.Context, q *db.Queries, row db.Job, route db.ConversationRouteRow, owner int64) (control.MirrorChoice, error) {
	key, err := q.MirrorReturnKey(ctx, db.MirrorReturnKeyParams{AccountID: row.AccountID, WhatsappChat: route.WhatsappChat})
	if err != nil {
		return control.MirrorChoice{}, err
	}
	c := control.MirrorChoice{ID: groupToken(), AccountID: row.AccountID, ChatID: route.WhatsappChat, OwnerID: owner, FromChat: route.TelegramChatID, FromThread: route.ThreadID, ReturnKey: key, ExpiresAt: time.Now().Add(24 * time.Hour).UnixMilli()}
	b, _ := json.Marshal(c)
	err = q.CreateMirrorChoice(ctx, db.CreateMirrorChoiceParams{AccountID: row.AccountID, ChoiceID: c.ID, Payload: b})
	return c, err
}
func validChoice(ctx context.Context, q *db.Queries, account string, c control.MirrorChoice, owner int64) (bool, error) {
	if c.AccountID != account || c.OwnerID != owner || c.ExpiresAt < time.Now().UnixMilli() || !strings.HasSuffix(c.ChatID, "@g.us") {
		return false, nil
	}
	r, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: account, WhatsappChat: c.ChatID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	key, err := q.MirrorReturnKey(ctx, db.MirrorReturnKeyParams{AccountID: account, WhatsappChat: c.ChatID})
	return r.TelegramChatID == c.FromChat && r.ThreadID == c.FromThread && key == c.ReturnKey, err
}
func pickerRequest(ctx context.Context, q *db.Queries, account, id string, owner int64) (control.MirrorChoice, int32, bool, error) {
	n, err := strconv.ParseInt(id, 10, 32)
	if err != nil || n <= 0 {
		return control.MirrorChoice{}, 0, false, nil
	}
	r, err := q.MirrorChatRequest(ctx, db.MirrorChatRequestParams{AccountID: account, RequestID: int32(n)})
	if errors.Is(err, pgx.ErrNoRows) {
		return control.MirrorChoice{}, int32(n), false, nil
	}
	if err != nil {
		return control.MirrorChoice{}, 0, false, err
	}
	var c control.MirrorChoice
	if json.Unmarshal(r.Payload, &c) != nil {
		return c, 0, false, delivery.ErrInvalid
	}
	valid, err := validChoice(ctx, q, account, c, owner)
	return c, int32(n), valid && !r.UsedJobID.Valid, err
}
func (s *Store) PrepareGroupControl(ctx context.Context, j delivery.Job, username string, c control.GroupSettings) (control.Plan, error) {
	if !validGroupSettings(c) {
		return control.Plan{}, delivery.ErrInvalid
	}
	var result control.Plan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "tg" {
			return delivery.ErrInvalid
		}
		var source struct {
			Bot    string          `json:"bot"`
			Update json.RawMessage `json:"update"`
		}
		var u models.Update
		if json.Unmarshal(row.Payload, &source) != nil || json.Unmarshal(source.Update, &u) != nil || !ingress.AuthorizedUpdate(source.Update, &u, c.OwnerID) || row.Dedup != "tg:"+source.Bot+":"+strconv.FormatInt(u.ID, 10) {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, known, err := existingGroupPlan(ctx, q, row)
		if err != nil {
			return err
		}
		if known {
			result = p
			return nil
		}
		p = control.Plan{Key: row.Dedup, Target: control.Target{BotID: source.Bot}, ReplyTTLSeconds: c.CommandTTL}
		if callback := u.CallbackQuery; callback != nil {
			if err := prepareTopicCallback(ctx, q, row, &p, source.Bot, callback, c); err != nil {
				return err
			}
		} else {
			m := u.Message
			if m == nil || u.EditedMessage != nil || (m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != c.OwnerID) {
				return delivery.ErrInvalid
			}
			context, err := operatorContext(ctx, q, row.AccountID, source.Bot, m, c.Management)
			if err != nil {
				return err
			}
			p.Target = context.Target
			p.Command = &control.Command{MessageID: int64(m.ID), Date: int64(m.Date) * 1000, TTLSeconds: c.CommandTTL}
			if m.ChatShared != nil {
				if source.Bot != "master" || m.Chat.Type != models.ChatTypePrivate {
					return delivery.ErrInvalid
				}
				shared := m.ChatShared
				choice, _, valid, err := pickerRequest(ctx, q, row.AccountID, strconv.Itoa(shared.RequestID), c.OwnerID)
				if err != nil {
					return err
				}
				if !valid || shared.ChatID >= 0 {
					p.Packets = []control.Packet{{Text: control.GroupSelectionExpired}}
				} else {
					event := control.MirrorEvent{ID: row.Dedup, Type: "link_group", At: time.Now().UnixMilli()}
					event.Data.ChatID = choice.ChatID
					event.Data.TelegramChat = shared.ChatID
					event.Data.RequestID = strconv.Itoa(shared.RequestID)
					b, _ := json.Marshal(event)
					if err := enqueueTransitionEvent(ctx, q, row.AccountID, "management:"+row.Dedup, b); err != nil {
						return err
					}
				}
			} else {
				parsed, ok := control.ParseCommand(m)
				if !ok {
					return delivery.ErrInvalid
				}
				if parsed.Name == "group" {
					text := ""
					if !context.Known {
						text = "Use this command inside a linked conversation or its management topic."
					} else if source.Bot != "master" {
						text = "Unknown command. Use /start in the master bot for help."
					} else if !strings.HasSuffix(context.Route.ChatID, "@g.us") {
						text = "Use /group in a WhatsApp group’s inbox topic."
					}
					if text != "" {
						p.Packets = []control.Packet{{Text: text}}
					} else {
						if username == "" {
							return delivery.ErrLocalRetry
						}
						route, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: context.Route.ChatID})
						if err != nil {
							return err
						}
						choice, err := createMirrorChoice(ctx, q, row, route, c.OwnerID)
						if err != nil {
							return err
						}
						p.Target = control.Target{BotID: "master", ChatID: route.TelegramChatID, ThreadID: route.ThreadID}
						p.ReplyTTLSeconds = c.ControlTTL
						// In a member DM the command and the route notice have distinct scopes.
						if m.Chat.ID != p.Target.ChatID {
							return delivery.ErrInvalid
						}
						p.Packets = []control.Packet{{Text: control.GroupButtonText, Markup: &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: "Create / link Telegram group", URL: "https://t.me/" + username + "?start=mirror_" + choice.ID}}}}}}
					}
				} else if parsed.Name == "start" && source.Bot == "master" && m.Chat.Type == models.ChatTypePrivate && len(parsed.Args) > 0 && strings.HasPrefix(parsed.Args[0], "mirror_") {
					id := strings.TrimPrefix(parsed.Args[0], "mirror_")
					b, err := q.MirrorChoice(ctx, db.MirrorChoiceParams{AccountID: row.AccountID, ChoiceID: id})
					valid := false
					var choice control.MirrorChoice
					if err == nil {
						if json.Unmarshal(b, &choice) != nil {
							return delivery.ErrInvalid
						}
						valid, err = validChoice(ctx, q, row.AccountID, choice, c.OwnerID)
					}
					if err != nil && !errors.Is(err, pgx.ErrNoRows) {
						return err
					}
					if !valid {
						p.Packets = []control.Packet{{Text: control.GroupButtonExpired}}
					} else {
						requestID := int32(0)
						for requestID == 0 {
							n, err := rand.Int(rand.Reader, big.NewInt(2147483646))
							if err != nil {
								return err
							}
							candidate := int32(n.Int64() + 1)
							payload, _ := json.Marshal(choice)
							inserted, err := q.CreateMirrorChatRequest(ctx, db.CreateMirrorChatRequestParams{AccountID: row.AccountID, RequestID: candidate, Payload: payload})
							if err != nil {
								return err
							}
							if inserted == 1 {
								requestID = candidate
							}
						}
						route, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: choice.ChatID})
						if err != nil {
							return err
						}
						name := route.Name
						if name == "" {
							name = choice.ChatID
						}
						p.ReplyTTLSeconds = c.ControlTTL
						p.Packets = []control.Packet{{Text: "Choose or create a private Telegram group for " + name + ". Confirm Bifrost’s administrator permissions. If your Telegram client has no create option in the picker, create a group first, then select it here.", Keyboard: control.PrivateGroupKeyboard(requestID)}}
					}
				} else {
					return delivery.ErrUnsupported
				}
			}
		}
		if err := saveGroupPlan(ctx, q, row, p); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}

func prepareTopicCallback(ctx context.Context, q *db.Queries, row db.Job, p *control.Plan, botID string, callback *models.CallbackQuery, c control.GroupSettings) error {
	p.Answer = &bot.AnswerCallbackQueryParams{CallbackQueryID: callback.ID}
	if callback.ID == "" {
		return delivery.ErrInvalid
	}
	parts := strings.Split(callback.Data, ":")
	if len(parts) != 2 {
		return delivery.ErrInvalid
	}
	payload, err := q.ControlAction(ctx, db.ControlActionParams{AccountID: row.AccountID, ActionID: parts[1]})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var a control.Action
	if json.Unmarshal(payload, &a) != nil {
		return delivery.ErrInvalid
	}
	m := callback.Message.Message
	if callback.Message.Type != models.MaybeInaccessibleMessageTypeMessage || m == nil {
		return nil
	}
	context, err := operatorContext(ctx, q, row.AccountID, botID, m, c.Management)
	if err != nil {
		return err
	}
	p.Target = context.Target
	if !context.Known || a.Kind != "operator" || a.AccountID != row.AccountID || a.OwnerID != c.OwnerID || a.BotID != botID || a.Operation != parts[0] || a.ChatID != context.Target.ChatID || a.ThreadID != context.Target.ThreadID || a.WhatsAppChat != context.Route.ChatID || a.ExpiresAt < time.Now().UnixMilli() {
		return nil
	}
	key, err := q.MirrorReturnKey(ctx, db.MirrorReturnKeyParams{AccountID: row.AccountID, WhatsappChat: a.WhatsAppChat})
	if err != nil {
		return err
	}
	if key != a.ReturnKey {
		return nil
	}
	event := control.OperatorEvent{ID: row.Dedup, Type: "operator_action", At: time.Now().UnixMilli()}
	event.Data.Operation = a.Operation
	event.Data.ChatID = a.WhatsAppChat
	event.Data.Sender = a.WhatsAppChat
	if a.Operation == "read" {
		event.Type = "mark_read"
	}
	b, _ := json.Marshal(event)
	return enqueueTransitionEvent(ctx, q, row.AccountID, "management:"+row.Dedup, b)
}
