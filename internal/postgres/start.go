package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func prepareStart(ctx context.Context, q *db.Queries, row db.Job, p *control.Plan) error {
	r := p.Start
	if r == nil {
		return nil
	}
	if row.Kind != "tg" || r.OwnerID <= 0 || r.ManagementChatID >= 0 || r.ControlTTLSeconds < 5 || r.ControlTTLSeconds > 86400 || p.Command == nil || p.Operator != nil || p.Clear != nil || p.Scope != nil || len(p.Packets) > 0 || len(p.Actions) > 0 || p.Answer != nil {
		return delivery.ErrInvalid
	}
	var source struct {
		Bot    string          `json:"bot"`
		Update json.RawMessage `json:"update"`
	}
	var u models.Update
	if json.Unmarshal(row.Payload, &source) != nil || json.Unmarshal(source.Update, &u) != nil || !ingress.AuthorizedUpdate(source.Update, &u, r.OwnerID) || u.Message == nil || u.EditedMessage != nil || source.Bot != p.Target.BotID || row.Dedup != "tg:"+source.Bot+":"+strconv.FormatInt(u.ID, 10) || int64(u.Message.ID) != p.Command.MessageID {
		return delivery.ErrInvalid
	}
	m := u.Message
	parsed, ok := control.ParseCommand(m)
	private := m.Chat.Type == models.ChatTypePrivate
	if !ok || parsed.Name != "start" || (private && m.Chat.ID != r.OwnerID) || (parsed.Recipient == "" && !private && source.Bot != "master") || control.DelegatedStart(source.Bot, private, parsed.Args) {
		return delivery.ErrInvalid
	}
	c, err := operatorContext(ctx, q, row.AccountID, source.Bot, m, r.ManagementChatID)
	if err != nil {
		return err
	}
	if c.Target != p.Target {
		return delivery.ErrLocalRetry
	}
	text := control.MasterGroupHelp
	if source.Bot == "master" {
		if private {
			text = control.MasterHelp
		}
		p.Packets = []control.Packet{{Text: text}}
		return nil
	}
	registered, err := q.IngressBot(ctx, db.IngressBotParams{AccountID: row.AccountID, BotID: source.Bot})
	if err != nil {
		return err
	}
	if !private {
		packet := control.Packet{Text: control.MemberGroupHelp}
		if registered.Username != "" {
			packet.Markup = &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: "Open private chat", URL: "https://t.me/" + registered.Username + "?start=bridge"}}}}
			p.ReplyTTLSeconds = r.ControlTTLSeconds
		}
		p.Packets = []control.Packet{packet}
		return nil
	}
	if _, err := q.SetBotStarted(ctx, db.SetBotStartedParams{AccountID: row.AccountID, BotID: source.Bot}); err != nil {
		return err
	}
	text = control.MemberUnbound
	contact, err := q.ContactForBot(ctx, db.ContactForBotParams{AccountID: row.AccountID, BotID: source.Bot})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		chat, err := conversationKey(ctx, q, row.AccountID, contact, d)
		if err != nil {
			return err
		}
		name := d.Person(contact, nil).Name
		if name == "" {
			name = chat
		}
		// A started member bot's missing direct route needs no forum topic.
		_, err = q.CreateInbox(ctx, db.CreateInboxParams{AccountID: row.AccountID, WhatsappChat: chat, Name: name, TelegramChatID: r.ManagementChatID, Mode: "dedicated", InboxRetired: true})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"chatId": chat})
		_, err = q.Enqueue(ctx, db.EnqueueParams{AccountID: row.AccountID, Dedup: "retire-inbox:" + row.Dedup, Stream: "retire-inbox:" + chat, Kind: "control-retire-inbox", Payload: payload})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		text = control.MemberReady
	}
	p.Packets = []control.Packet{{Text: text}}
	return nil
}
