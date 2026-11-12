package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/bots"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/metadata"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func validProvisionSettings(c bots.Settings) bool {
	return c.Session != "" && c.OwnerID > 0 && c.Management < 0 && c.CommandTTL >= 5 && c.CommandTTL <= 86400 && c.ControlTTL >= 5 && c.ControlTTL <= 86400
}
func (s *Store) PrepareCreatedBotControl(ctx context.Context, j delivery.Job, c bots.Settings) (control.Plan, error) {
	var result control.Plan
	if !validProvisionSettings(c) {
		return result, delivery.ErrInvalid
	}
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "control-bot-created" {
			return delivery.ErrInvalid
		}
		var source struct{ Bot string }
		if json.Unmarshal(row.Payload, &source) != nil || source.Bot == "" || source.Bot == "master" || row.Dedup != "created-bot:"+source.Bot {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if p, found, err := existingGroupPlan(ctx, q, row); err != nil || found {
			result = p
			return err
		}
		registered, err := q.IngressBot(ctx, db.IngressBotParams{AccountID: row.AccountID, BotID: source.Bot})
		if err != nil {
			return err
		}
		if registered.Username == "" {
			return delivery.ErrInvalid
		}
		credential, err := q.BotCredential(ctx, db.BotCredentialParams{AccountID: row.AccountID, BotID: source.Bot})
		if err != nil {
			return err
		}
		if !credential.CreatedNotice {
			return delivery.ErrInvalid
		}
		p := control.Plan{Key: row.Dedup, Target: control.Target{BotID: "master", ChatID: c.OwnerID}, ReplyTTLSeconds: c.ControlTTL}
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		pending, err := q.ContactProvisions(ctx, row.AccountID)
		if err != nil {
			return err
		}
		var buttons [][]models.InlineKeyboardButton
		seen := map[string]bool{}
		for _, request := range pending {
			person := d.Person(request.PersonID, nil)
			if request.State != "pending" || request.SessionID != c.Session || person.Registered || seen[person.ID] || len(buttons) >= 20 {
				continue
			}
			seen[person.ID] = true
			id := groupToken()
			expires := time.Now().Add(time.Duration(c.ControlTTL) * time.Second).UnixMilli()
			if err := q.CreateContactBindChoice(ctx, db.CreateContactBindChoiceParams{AccountID: row.AccountID, ChoiceID: id, PersonID: person.ID, BotID: source.Bot, SessionID: c.Session, OwnerID: c.OwnerID, PlanKey: row.Dedup, ExpiresAt: expires}); err != nil {
				return err
			}
			name := person.Name
			if name == "" {
				name = person.ID
			}
			buttons = append(buttons, []models.InlineKeyboardButton{{Text: name, CallbackData: "bind:" + id}})
		}
		packet := control.Packet{Text: "Created @" + registered.Username + ". Choose the WhatsApp contact to bind it to."}
		if len(buttons) > 0 {
			packet.Markup = &models.InlineKeyboardMarkup{InlineKeyboard: buttons}
		}
		p.Packets = []control.Packet{packet}
		if err := saveGroupPlan(ctx, q, row, p); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}

func provisionRequest(ctx context.Context, q *db.Queries, row db.Job, c bots.Settings, person, chat, manager string, p *control.Plan) error {
	if !identity.IsPerson(person) {
		p.Packets = []control.Packet{{Text: "Choose a group participant to create a contact bot"}}
		return nil
	}
	d, err := directory(ctx, q, row.AccountID)
	if err != nil {
		return err
	}
	person = d.Canonical(person)
	contact := d.Person(person, nil)
	if contact.Registered {
		p.Packets = []control.Packet{{Text: "This person already has @" + contact.Bot.Username + ". Add that bot to this group to give it priority."}}
		return nil
	}
	name := contact.Name
	if name == "" {
		name = person
	}
	if err := q.SaveContactProfile(ctx, db.SaveContactProfileParams{AccountID: row.AccountID, PersonID: person, Name: name}); err != nil {
		return err
	}
	if err := q.RequestContactProvision(ctx, db.RequestContactProvisionParams{AccountID: row.AccountID, PersonID: person, SessionID: c.Session, ChatID: chat}); err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(person + strconv.FormatInt(c.OwnerID, 10)))
	username := "wa_" + hex.EncodeToString(hash[:])[:16] + "_bot"
	// JavaScript encodeURIComponent escapes spaces as %20, unlike query forms.
	escaped := strings.ReplaceAll(url.QueryEscape(metadata.Clip(name, 64)), "+", "%20")
	link := "https://t.me/newbot/" + manager + "/" + username + "?name=" + escaped
	p.ReplyTTLSeconds = c.ControlTTL
	p.Packets = []control.Packet{{Text: "Create this contact’s bot, then confirm its contact binding in your private chat with the master. If Telegram refuses creation at your bot limit, messages continue through the master.", Markup: &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: "Create contact bot", URL: link}}}}}}
	return nil
}

func provisionCommand(ctx context.Context, q *db.Queries, row db.Job, sourceBot string, u models.Update, c bots.Settings, manager string, p *control.Plan) error {
	m := u.Message
	if m == nil || u.EditedMessage != nil || m.From == nil || m.From.ID != c.OwnerID || m.From.IsBot || (m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != c.OwnerID) {
		return delivery.ErrInvalid
	}
	command, ok := control.ParseCommand(m)
	if !ok || command.Name != "create" || (command.Recipient == "" && m.Chat.Type != models.ChatTypePrivate && sourceBot != "master") {
		return delivery.ErrInvalid
	}
	current, err := operatorContext(ctx, q, row.AccountID, sourceBot, m, c.Management)
	if err != nil {
		return err
	}
	p.Target = current.Target
	p.Command = &control.Command{MessageID: int64(m.ID), Date: int64(m.Date) * 1000, TTLSeconds: c.CommandTTL}
	if !current.Known {
		p.Packets = []control.Packet{{Text: "Use this command inside a linked conversation or its management topic."}}
		return nil
	}
	person := current.Route.ChatID
	if reply := m.ReplyToMessage; reply != nil {
		mapped, err := q.TelegramMapping(ctx, db.TelegramMappingParams{AccountID: row.AccountID, Scope: relayScope(sourceBot, m.Chat.ID), TelegramChatID: m.Chat.ID, TelegramMessageID: int64(reply.ID)})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil && (mapped.WhatsappChat != current.Route.ChatID || mapped.ThreadID != current.Target.ThreadID || mapped.Deleted || mapped.ObservationDeleted) {
			return delivery.ErrInvalid
		}
	}
	if strings.HasSuffix(person, "@g.us") {
		if m.ReplyToMessage == nil {
			p.Packets = []control.Packet{{Text: "Reply to a participant’s message with /create to create their bot."}}
			return nil
		}
		mapped, err := q.TelegramMapping(ctx, db.TelegramMappingParams{AccountID: row.AccountID, Scope: relayScope(sourceBot, m.Chat.ID), TelegramChatID: m.Chat.ID, TelegramMessageID: int64(m.ReplyToMessage.ID)})
		if errors.Is(err, pgx.ErrNoRows) {
			p.Packets = []control.Packet{{Text: "Reply to a participant’s message with /create to create their bot."}}
			return nil
		}
		if err != nil {
			return err
		}
		raw, err := q.ProvisionMessageSender(ctx, db.ProvisionMessageSenderParams{AccountID: row.AccountID, WhatsappChat: current.Route.ChatID, MessageID: mapped.WhatsappMessageID})
		if errors.Is(err, pgx.ErrNoRows) {
			p.Packets = []control.Packet{{Text: "Reply to a participant’s message with /create to create their bot."}}
			return nil
		}
		if err != nil {
			return err
		}
		var source identity.Source
		if json.Unmarshal(raw, &source) != nil {
			return delivery.ErrInvalid
		}
		if !identity.IsPerson(source.SenderID) {
			p.Packets = []control.Packet{{Text: "Reply to a participant’s message with /create to create their bot."}}
			return nil
		}
		person = source.SenderID
	}
	return provisionRequest(ctx, q, row, c, person, current.Route.ChatID, manager, p)
}

func bindProvision(ctx context.Context, q *db.Queries, row db.Job, sourceBot string, callback *models.CallbackQuery, c bots.Settings, p *control.Plan) error {
	p.Target = control.Target{BotID: "master", ChatID: c.OwnerID}
	p.Answer = &bot.AnswerCallbackQueryParams{CallbackQueryID: callback.ID, Text: "This binding choice expired or is no longer pending."}
	parts := strings.Split(callback.Data, ":")
	if len(parts) != 2 || parts[0] != "bind" {
		return delivery.ErrInvalid
	}
	if sourceBot != "master" || callback.From.ID != c.OwnerID || callback.From.IsBot || callback.Message.Type != models.MaybeInaccessibleMessageTypeMessage || callback.Message.Message == nil {
		return nil
	}
	message := callback.Message.Message
	if message.Chat.ID != c.OwnerID || message.Chat.Type != models.ChatTypePrivate || message.MessageThreadID != 0 {
		return nil
	}
	choice, err := q.ContactBindChoice(ctx, db.ContactBindChoiceParams{AccountID: row.AccountID, ChoiceID: parts[1]})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if choice.SessionID != c.Session || choice.OwnerID != c.OwnerID || choice.ExpiresAt < time.Now().UnixMilli() || choice.Used {
		return nil
	}
	effect, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: choice.PlanKey + ":text:0"})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var receipt models.Message
	if effect.State != "done" || json.Unmarshal(effect.Result, &receipt) != nil || receipt.ID != message.ID || receipt.Chat.ID != message.Chat.ID {
		return nil
	}
	d, err := directory(ctx, q, row.AccountID)
	if err != nil {
		return err
	}
	pending, err := q.ContactProvisions(ctx, row.AccountID)
	if err != nil {
		return err
	}
	person := d.Canonical(choice.PersonID)
	requestID := ""
	for _, request := range pending {
		if request.State == "pending" && request.SessionID == c.Session && d.Canonical(request.PersonID) == person {
			requestID = request.PersonID
			break
		}
	}
	if requestID == "" {
		return nil
	}
	p.Answer.Text = ""
	contact := d.Person(person, nil)
	if contact.Registered && contact.Bot.ID != choice.BotID {
		p.Packets = []control.Packet{{Text: "Provisioning request is no longer pending"}}
		return nil
	}
	bindings, err := q.IdentityBindings(ctx, row.AccountID)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.BotID == choice.BotID && d.Canonical(binding.ContactID) != person {
			p.Packets = []control.Packet{{Text: "This bot is already bound to another person"}}
			return nil
		}
	}
	registered, err := q.IngressBot(ctx, db.IngressBotParams{AccountID: row.AccountID, BotID: choice.BotID})
	if err != nil {
		return err
	}
	if registered.Username == "" {
		return delivery.ErrInvalid
	}
	if err := q.BindContactBot(ctx, db.BindContactBotParams{AccountID: row.AccountID, ContactID: person, BotID: choice.BotID}); err != nil {
		return err
	}
	if _, err := directory(ctx, q, row.AccountID); err != nil {
		return err
	}
	chat, err := conversationKey(ctx, q, row.AccountID, person, d)
	if err != nil {
		return err
	}
	if err := q.BindDirectRouteMode(ctx, db.BindDirectRouteModeParams{AccountID: row.AccountID, WhatsappChat: chat}); err != nil {
		return err
	}
	if err := q.BindContactProvision(ctx, db.BindContactProvisionParams{AccountID: row.AccountID, PersonID: requestID}); err != nil {
		return err
	}
	if err := q.UseContactBindChoice(ctx, db.UseContactBindChoiceParams{AccountID: row.AccountID, ChoiceID: choice.ChoiceID}); err != nil {
		return err
	}
	payload := map[string]any{"type": "sync_metadata", "data": map[string]string{"scope": "contact", "id": person, "source": "bind"}}
	if _, err := metadataEnqueue(ctx, q, row.AccountID, "binding-sync:"+row.Dedup, "metadata-request:"+row.Dedup, "sync-request", payload); err != nil {
		return err
	}
	native, err := q.IdentityBots(ctx, row.AccountID)
	if err != nil {
		return err
	}
	for _, b := range native {
		if b.BotID == choice.BotID && b.Started {
			if _, err := metadataEnqueue(ctx, q, row.AccountID, "retire-inbox:"+row.Dedup, "retire-inbox:"+chat, "control-retire-inbox", map[string]string{"chatId": chat}); err != nil {
				return err
			}
		}
	}
	name := contact.Name
	if name == "" {
		name = person
	}
	p.ReplyTTLSeconds = c.ControlTTL
	p.Packets = []control.Packet{{Text: "Bound @" + registered.Username + " to " + name + ". Start it for private chat, and add it to any mirrored groups where you want its identity.", Markup: &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: "Start contact bot", URL: "https://t.me/" + registered.Username + "?start=bridge"}}, {{Text: "Add to a group", URL: "https://t.me/" + registered.Username + "?startgroup=bridge"}}}}}}
	return nil
}

func (s *Store) PrepareProvisionControl(ctx context.Context, j delivery.Job, manager string, c bots.Settings) (control.Plan, error) {
	var result control.Plan
	if !validProvisionSettings(c) || manager == "" {
		return result, delivery.ErrInvalid
	}
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "tg" && row.Kind != "management" {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if p, found, err := existingGroupPlan(ctx, q, row); err != nil || found {
			result = p
			return err
		}
		p := control.Plan{Key: row.Dedup, Target: control.Target{BotID: "master"}, ReplyTTLSeconds: c.CommandTTL}
		if row.Kind == "management" {
			var event control.OperatorEvent
			if json.Unmarshal(row.Payload, &event) != nil || event.Type != "operator_action" || event.Data.Operation != "create" {
				return delivery.ErrUnsupported
			}
			route, err := q.ConversationRoute(ctx, db.ConversationRouteParams{AccountID: row.AccountID, WhatsappChat: event.Data.ChatID})
			if err != nil {
				return err
			}
			p.Target = control.Target{BotID: "master", ChatID: route.TelegramChatID, ThreadID: route.ThreadID}
			if err := provisionRequest(ctx, q, row, c, event.Data.Sender, event.Data.ChatID, manager, &p); err != nil {
				return err
			}
		} else {
			var source struct {
				Bot    string
				Update json.RawMessage
			}
			var u models.Update
			if json.Unmarshal(row.Payload, &source) != nil || json.Unmarshal(source.Update, &u) != nil || !ingress.AuthorizedUpdate(source.Update, &u, c.OwnerID) || row.Dedup != "tg:"+source.Bot+":"+strconv.FormatInt(u.ID, 10) {
				return delivery.ErrInvalid
			}
			if u.CallbackQuery != nil {
				if err := bindProvision(ctx, q, row, source.Bot, u.CallbackQuery, c, &p); err != nil {
					return err
				}
			} else {
				if err := provisionCommand(ctx, q, row, source.Bot, u, c, manager, &p); err != nil {
					return err
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

var _ bots.ProvisionStore = (*Store)(nil)
