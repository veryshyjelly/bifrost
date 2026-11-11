package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/history"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

const historyBusy = "History is already importing for this conversation. Wait for it to finish, then run /history again."
const historyExpired = "This history choice expired or its conversation changed. Run /history again."

func validHistorySettings(c history.Settings) bool {
	return c.Session != "" && c.OwnerID > 0 && c.Management < 0 && c.DefaultCount >= 1 && c.DefaultCount <= 2000 && c.CommandTTL >= 5 && c.CommandTTL <= 86400 && c.ControlTTL >= 5 && c.ControlTTL <= 86400
}
func historyRoute(r db.ConversationRouteRow) conversation.Route {
	return conversation.Route{ChatID: r.WhatsappChat, Name: r.Name, TelegramChatID: r.TelegramChatID, ThreadID: r.ThreadID, Mode: conversation.Mode(r.Mode), InboxRetired: r.InboxRetired}
}
func saveHistoryChoice(ctx context.Context, q *db.Queries, account string, c history.Choice) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return q.SaveHistoryChoice(ctx, db.SaveHistoryChoiceParams{AccountID: account, ChoiceID: c.ID, Payload: raw})
}
func historyPrompt(ctx context.Context, q *db.Queries, row db.Job, p *control.Plan, c history.Settings, route conversation.Route, count int, bound string) error {
	active, err := q.ActiveHistory(ctx, db.ActiveHistoryParams{AccountID: row.AccountID, WhatsappChat: route.ChatID})
	if err != nil {
		return err
	}
	if active {
		p.Packets = []control.Packet{{Text: historyBusy}}
		return nil
	}
	key, err := q.MirrorReturnKey(ctx, db.MirrorReturnKeyParams{AccountID: row.AccountID, WhatsappChat: route.ChatID})
	if err != nil {
		return err
	}
	choice := history.Choice{ID: metadataRunKey(row.AccountID, c.Session, row.Dedup), Session: c.Session, ChatID: route.ChatID, State: "pending", PlanKey: row.Dedup, ReturnKey: key, BoundContact: bound, Target: p.Target, Route: route, Count: count, OwnerID: c.OwnerID, ExpiresAt: time.Now().Add(time.Duration(c.ControlTTL) * time.Second).UnixMilli()}
	if err := saveHistoryChoice(ctx, q, row.AccountID, choice); err != nil {
		return err
	}
	p.ReplyTTLSeconds = c.ControlTTL
	p.Packets = []control.Packet{{Text: fmt.Sprintf("Import up to %d WhatsApp messages here?\nSkip existing avoids copies recorded as delivered. Import all restores a manually cleared chat, but can create duplicates if messages are still present.", count), Markup: &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: "Skip existing", CallbackData: "history:skip:" + choice.ID}, {Text: "Import all", CallbackData: "history:all:" + choice.ID}}, {{Text: "Cancel", CallbackData: "history:cancel:" + choice.ID}}}}}}
	return nil
}
func prepareHistoryCommand(ctx context.Context, q *db.Queries, row db.Job, botID string, u models.Update, p *control.Plan, c history.Settings) error {
	m := u.Message
	if m == nil || u.EditedMessage != nil || m.From == nil || m.From.ID != c.OwnerID || m.From.IsBot || (m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != c.OwnerID) {
		return delivery.ErrInvalid
	}
	command, ok := control.ParseCommand(m)
	if !ok || command.Name != "history" || (command.Recipient == "" && m.Chat.Type != models.ChatTypePrivate && botID != "master") {
		return delivery.ErrInvalid
	}
	context, err := operatorContext(ctx, q, row.AccountID, botID, m, c.Management)
	if err != nil {
		return err
	}
	p.Target = context.Target
	p.Command = &control.Command{MessageID: int64(m.ID), Date: int64(m.Date) * 1000, TTLSeconds: c.CommandTTL}
	count, valid := history.Count(command.Args, c.DefaultCount)
	if !valid {
		p.Packets = []control.Packet{{Text: "Use /history <n>, where n is 1 to 2000."}}
		return nil
	}
	bound := ""
	if m.Chat.Type == models.ChatTypePrivate && botID != "master" {
		contact, err := q.ContactForBot(ctx, db.ContactForBotParams{AccountID: row.AccountID, BotID: botID})
		if errors.Is(err, pgx.ErrNoRows) {
			p.Packets = []control.Packet{{Text: "Use /history <n> inside a mirror group, a linked management topic, or the contact’s bot DM."}}
			return nil
		}
		if err != nil {
			return err
		}
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		bound = d.Canonical(contact)
		if _, err := q.SetBotStarted(ctx, db.SetBotStartedParams{AccountID: row.AccountID, BotID: botID}); err != nil {
			return err
		}
		if !context.Known {
			chat, err := conversationKey(ctx, q, row.AccountID, contact, d)
			if err != nil {
				return err
			}
			name := d.Person(contact, nil).Name
			if name == "" {
				name = chat
			}
			if _, err := q.CreateInbox(ctx, db.CreateInboxParams{AccountID: row.AccountID, WhatsappChat: chat, Name: name, TelegramChatID: c.Management, Mode: "dedicated", InboxRetired: true}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			context, err = operatorContext(ctx, q, row.AccountID, botID, m, c.Management)
			if err != nil {
				return err
			}
		}
	}
	if !context.Known || context.Route.ChatID == "status@broadcast" {
		p.Packets = []control.Packet{{Text: "Use /history <n> inside a mirror group, a linked management topic, or the contact’s bot DM."}}
		return nil
	}
	if context.Route.Mode == conversation.Muted || context.Route.Mode == conversation.Ignored {
		p.Packets = []control.Packet{{Text: "Use /keep to resume this conversation before importing history."}}
		return nil
	}
	if m.ReplyToMessage != nil {
		mapping, err := q.TelegramMapping(ctx, db.TelegramMappingParams{AccountID: row.AccountID, Scope: relayScope(botID, m.Chat.ID), TelegramChatID: m.Chat.ID, TelegramMessageID: int64(m.ReplyToMessage.ID)})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil && (mapping.WhatsappChat != context.Route.ChatID || mapping.ThreadID != context.Target.ThreadID || mapping.Deleted || mapping.ObservationDeleted) {
			return delivery.ErrInvalid
		}
	}
	return historyPrompt(ctx, q, row, p, c, context.Route, count, bound)
}
func expireHistoryPrompt(ctx context.Context, q *db.Queries, row db.Job, choice history.Choice, message int) error {
	source, err := q.ControlPlan(ctx, db.ControlPlanParams{AccountID: row.AccountID, PlanKey: choice.PlanKey})
	if err != nil {
		return err
	}
	plan, err := decodeControl(source)
	if err != nil {
		return err
	}
	if plan.Target != choice.Target {
		return delivery.ErrInvalid
	}
	if err := recordControl(ctx, q, row.AccountID, plan, 0); err != nil {
		return err
	}
	if err := q.ExpireHistoryPrompt(ctx, db.ExpireHistoryPromptParams{AccountID: row.AccountID, Scope: relayScope(choice.Target.BotID, choice.Target.ChatID), ChatID: choice.Target.ChatID, MessageID: int64(message), ExpiresAt: time.Now().UnixMilli()}); err != nil {
		return err
	}
	_, err = metadataEnqueue(ctx, q, row.AccountID, "history-choice-delete:"+choice.ID, "history-choice-delete:"+choice.ID, "control-expire", map[string]any{"scope": relayScope(choice.Target.BotID, choice.Target.ChatID), "bot": choice.Target.BotID, "chat": choice.Target.ChatID, "message": message})
	return err
}
func selectHistory(ctx context.Context, q *db.Queries, row db.Job, botID string, callback *models.CallbackQuery, p *control.Plan, c history.Settings) error {
	p.Target = control.Target{BotID: botID}
	p.Answer = &bot.AnswerCallbackQueryParams{CallbackQueryID: callback.ID, Text: historyExpired}
	parts := strings.Split(callback.Data, ":")
	if !history.Callback(callback.Data) {
		return delivery.ErrInvalid
	}
	if callback.From.ID != c.OwnerID || callback.From.IsBot || callback.Message.Type != models.MaybeInaccessibleMessageTypeMessage || callback.Message.Message == nil {
		return nil
	}
	m := callback.Message.Message
	raw, err := q.HistoryChoice(ctx, db.HistoryChoiceParams{AccountID: row.AccountID, ChoiceID: parts[2]})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var choice history.Choice
	if json.Unmarshal(raw, &choice) != nil {
		return delivery.ErrInvalid
	}
	current, err := operatorContext(ctx, q, row.AccountID, botID, m, c.Management)
	if err != nil {
		return err
	}
	if choice.ID != parts[2] || choice.OwnerID != c.OwnerID || choice.Session != c.Session || choice.ExpiresAt < time.Now().UnixMilli() || current.Target != choice.Target || !current.Known || current.Route.ChatID != choice.ChatID || current.Route.Mode == conversation.Muted || current.Route.Mode == conversation.Ignored {
		return nil
	}
	if m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != c.OwnerID {
		return nil
	}
	key, err := q.MirrorReturnKey(ctx, db.MirrorReturnKeyParams{AccountID: row.AccountID, WhatsappChat: choice.ChatID})
	if err != nil {
		return err
	}
	if key != choice.ReturnKey {
		return nil
	}
	if choice.BoundContact != "" {
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		contact, err := q.ContactForBot(ctx, db.ContactForBotParams{AccountID: row.AccountID, BotID: botID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err != nil || d.Canonical(contact) != d.Canonical(choice.BoundContact) {
			return nil
		}
	}
	effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: choice.PlanKey + ":text:0"})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var receipt models.Message
	if effect.State != "done" || json.Unmarshal(effect.Result, &receipt) != nil || receipt.ID != m.ID || receipt.Chat.ID != m.Chat.ID {
		return nil
	}
	p.Target = choice.Target
	p.Answer.Text = ""
	if choice.State == "pending" {
		choice.Mode = parts[1]
		if parts[1] == "cancel" {
			choice.State = "cancelled"
		} else {
			active, err := q.ActiveHistory(ctx, db.ActiveHistoryParams{AccountID: row.AccountID, WhatsappChat: choice.ChatID})
			if err != nil {
				return err
			}
			if active {
				choice.State = "busy"
			} else {
				run := history.Run{ID: metadataRunKey(row.AccountID, c.Session, "history-choice:"+choice.ID), Session: c.Session, ChatID: choice.ChatID, Mode: parts[1], State: "queued", Timezone: c.Timezone, ReturnKey: choice.ReturnKey, Route: choice.Route, Target: choice.Target, Count: choice.Count}
				if run.Target.ChatID < 0 {
					run.Target.BotID = ""
				}
				raw, _ := json.Marshal(run)
				if err := q.CreateHistoryRun(ctx, db.CreateHistoryRunParams{AccountID: row.AccountID, RunID: run.ID, SessionID: run.Session, WhatsappChat: run.ChatID, Payload: raw}); err != nil {
					return err
				}
				n, err := q.RecordHistoryActivity(ctx, db.RecordHistoryActivityParams{AccountID: row.AccountID, WhatsappChat: run.ChatID, RunID: run.ID, Status: "queued"})
				if err != nil {
					return err
				}
				if n != 1 {
					return delivery.ErrLocalRetry
				}
				if _, err := metadataEnqueue(ctx, q, row.AccountID, "history-fetch:"+run.ID, "history-fetch:"+run.ID, "history", map[string]string{"run": run.ID, "session": c.Session}); err != nil {
					return err
				}
				choice.State, choice.RunID = "selected", run.ID
			}
		}
		if err := saveHistoryChoice(ctx, q, row.AccountID, choice); err != nil {
			return err
		}
		if err := expireHistoryPrompt(ctx, q, row, choice, m.ID); err != nil {
			return err
		}
	} else {
		p.Answer.Text = "This choice was already used. Run /history for a new import."
	}
	text := historyBusy
	if choice.State == "cancelled" {
		text = "History import cancelled."
	} else if choice.State == "selected" {
		mode := "Already delivered messages will be skipped."
		if choice.Mode == "all" {
			mode = "Previously delivered messages will be included."
		}
		text = fmt.Sprintf("History queued: fetching up to %d messages, then posting oldest first here. %s", choice.Count, mode)
	}
	p.Prefix = "history-choice-confirm:" + choice.ID
	p.Packets = []control.Packet{{Text: text}}
	return nil
}
func (s *Store) PrepareHistoryControl(ctx context.Context, j delivery.Job, c history.Settings) (control.Plan, error) {
	var result control.Plan
	if !validHistorySettings(c) {
		return result, delivery.ErrInvalid
	}
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "tg" {
			return delivery.ErrInvalid
		}
		var source struct {
			Bot    string
			Update json.RawMessage
		}
		var u models.Update
		if json.Unmarshal(row.Payload, &source) != nil || json.Unmarshal(source.Update, &u) != nil || !ingress.AuthorizedUpdate(source.Update, &u, c.OwnerID) || row.Dedup != "tg:"+source.Bot+":"+strconv.FormatInt(u.ID, 10) {
			return delivery.ErrInvalid
		}
		if err := q.LockQueueTopology(ctx); err != nil {
			return err
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if p, found, err := existingGroupPlan(ctx, q, row); err != nil || found {
			result = p
			return err
		}
		p := control.Plan{Key: row.Dedup, Target: control.Target{BotID: source.Bot}, ReplyTTLSeconds: c.CommandTTL}
		if u.CallbackQuery != nil {
			if err := selectHistory(ctx, q, row, source.Bot, u.CallbackQuery, &p, c); err != nil {
				return err
			}
		} else {
			if err := prepareHistoryCommand(ctx, q, row, source.Bot, u, &p, c); err != nil {
				return err
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
func (s *Store) PrepareHistoryPrompt(ctx context.Context, j delivery.Job, c history.Settings) (control.Plan, error) {
	var result control.Plan
	if !validHistorySettings(c) {
		return result, delivery.ErrInvalid
	}
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		var event control.OperatorEvent
		if row.Kind != "management" || json.Unmarshal(row.Payload, &event) != nil || event.Type != "operator_action" || event.Data.Operation != "history" {
			return delivery.ErrUnsupported
		}
		if err := q.LockQueueTopology(ctx); err != nil {
			return err
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if p, found, err := existingGroupPlan(ctx, q, row); err != nil || found {
			result = p
			return err
		}
		route, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: event.Data.ChatID})
		if err != nil {
			return err
		}
		p := control.Plan{Key: row.Dedup, Target: control.Target{BotID: "master", ChatID: route.TelegramChatID, ThreadID: route.ThreadID}, ReplyTTLSeconds: c.CommandTTL}
		if route.Mode == "muted" || route.Mode == "ignored" {
			p.Packets = []control.Packet{{Text: "Use /keep to resume this conversation before importing history."}}
		} else {
			d, err := directory(ctx, q, row.AccountID)
			if err != nil {
				return err
			}
			person := d.Person(route.WhatsappChat, nil)
			if route.Mode == "dedicated" && person.Registered && person.Bot.Started {
				p.Target = control.Target{BotID: person.Bot.ID, ChatID: c.OwnerID}
			}
			if err := historyPrompt(ctx, q, row, &p, c, historyRoute(route), c.DefaultCount, ""); err != nil {
				return err
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
