package mentions

import (
	"context"
	"crypto/rand"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/conversation"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"golang.org/x/text/cases"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

const pickerHelp = "Use /mention [name or number] in an active WhatsApp group’s management topic or Telegram mirror group."
const pickerExpired = "This picker expired. Use /mention again."
const pickerRosterError = "Could not refresh the WhatsApp member list. Try /sync, then send your message again."

type PickerOptions struct {
	OwnerID                int64
	ControlTTL, CommandTTL time.Duration
	Now                    func() time.Time
}
type Picker struct {
	mentions *Service
	control  *control.Service
	c        PickerOptions
}

func NewPicker(mentions *Service, controls *control.Service, c PickerOptions) (*Picker, error) {
	if c.ControlTTL == 0 {
		c.ControlTTL = 600 * time.Second
	}
	if c.CommandTTL == 0 {
		c.CommandTTL = 60 * time.Second
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if mentions == nil || controls == nil || c.OwnerID <= 0 || c.ControlTTL < 5*time.Second || c.ControlTTL > 24*time.Hour || c.CommandTTL < 5*time.Second || c.CommandTTL > 24*time.Hour || c.ControlTTL%time.Second != 0 || c.CommandTTL%time.Second != 0 {
		return nil, delivery.ErrInvalid
	}
	return &Picker{mentions: mentions, control: controls, c: c}, nil
}
func (p *Picker) Handle(ctx context.Context, job delivery.Job, e *delivery.Effects, botID string, u *models.Update) error {
	if job.Kind != "tg" || botID == "" || u == nil {
		return delivery.ErrInvalid
	}
	if u.CallbackQuery != nil {
		q := u.CallbackQuery
		if !strings.HasPrefix(q.Data, "mention:") {
			return delivery.ErrUnsupported
		}
		if q.From.IsBot || q.From.ID != p.c.OwnerID {
			return nil
		}
		if saved, ok, err := p.control.Prepared(ctx, job, job.Dedup); err != nil {
			return err
		} else if ok {
			return p.control.Send(ctx, job, e, saved)
		}
		return p.callback(ctx, job, e, botID, q)
	}
	if edited := u.EditedMessage; edited != nil {
		if edited.From == nil || edited.From.IsBot || edited.From.ID != p.c.OwnerID {
			return nil
		}
		if parsed, ok := control.ParseCommand(edited); ok && parsed.Name == "mention" {
			return nil
		}
		known, err := p.control.WasCommand(ctx, job, botID, edited)
		if err != nil {
			return err
		}
		if known {
			return nil
		}
		return delivery.ErrUnsupported
	}
	m := u.Message
	command, ok := control.ParseCommand(m)
	if !ok || command.Name != "mention" {
		return delivery.ErrUnsupported
	}
	if m.From == nil || m.From.IsBot || m.From.ID != p.c.OwnerID {
		return nil
	}
	if m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != p.c.OwnerID {
		return delivery.ErrInvalid
	}
	if saved, ok, err := p.control.Prepared(ctx, job, job.Dedup); err != nil {
		return err
	} else if ok {
		return p.control.Send(ctx, job, e, saved)
	}
	if command.Recipient == "" && m.Chat.Type != models.ChatTypePrivate && botID != "master" {
		return nil
	}
	addressed, err := p.control.Addressed(ctx, job, botID, command.Recipient)
	if err != nil {
		return err
	}
	if !addressed {
		return nil
	}
	c, err := p.control.Context(ctx, job, botID, m)
	if err != nil {
		return err
	}
	plan := control.Plan{Key: job.Dedup, Target: c.Target, ReplyTTLSeconds: int(p.c.CommandTTL / time.Second), Command: &control.Command{MessageID: int64(m.ID), Date: int64(m.Date) * 1000, TTLSeconds: int(p.c.CommandTTL / time.Second)}}
	if !activeGroup(c) {
		plan.Packets = []control.Packet{{Text: pickerHelp}}
		return p.control.Send(ctx, job, e, plan)
	}
	search := clipSearch(strings.Join(command.Args, " "), 128)
	if err := p.page(ctx, job, &plan, c, search, 1); err != nil {
		return err
	}
	return p.control.Send(ctx, job, e, plan)
}
func activeGroup(c control.Context) bool {
	return c.Known && strings.HasSuffix(c.Route.ChatID, "@g.us") && c.Route.Mode != conversation.Muted && c.Route.Mode != conversation.Ignored
}
func clipSearch(text string, limit int) string {
	units, end := 0, 0
	for i, r := range text {
		size := 1
		if r > 0xffff {
			size = 2
		}
		if units+size > limit {
			break
		}
		units += size
		end = i + len(string(r))
	}
	return text[:end]
}

type pickerRow struct{ id, name, username string }

func (p *Picker) page(ctx context.Context, job delivery.Job, plan *control.Plan, c control.Context, search string, page int) error {
	roster, err := p.mentions.Roster(ctx, job, c.Route.ChatID, true)
	if errors.Is(err, ErrRoster) {
		plan.Prefix = job.Dedup + ":error"
		plan.Packets = []control.Packet{{Text: pickerRosterError}}
		return nil
	}
	if err != nil {
		return err
	}
	d, err := p.mentions.store.IdentityDirectory(ctx, job.AccountID)
	if err != nil {
		return localError(err)
	}
	lower := cases.Lower(language.Und)
	needle := lower.String(search)
	rows := []pickerRow{}
	seen := map[string]bool{}
	for _, m := range roster.Members {
		id := d.Canonical(m.ID)
		if seen[id] {
			continue
		}
		seen[id] = true
		person := d.Person(id, roster.Members)
		if strings.Contains(lower.String(person.Name+" "+id+" "+person.Bot.Username), needle) {
			rows = append(rows, pickerRow{id, person.Name, person.Bot.Username})
		}
	}
	collator := collate.New(language.AmericanEnglish)
	sort.SliceStable(rows, func(i, j int) bool {
		cmp := collator.CompareString(rows[i].name, rows[j].name)
		if cmp == 0 {
			cmp = collator.CompareString(rows[i].id, rows[j].id)
		}
		return cmp < 0
	})
	pages := (len(rows) + 9) / 10
	if pages == 0 {
		pages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	text := "WhatsApp mentions · page " + strconv.Itoa(page) + "/" + strconv.Itoa(pages)
	if roster.Cached {
		text += "\nUsing the cached member list; refresh failed."
	}
	value := presentation.Plain(text + "\nCopy a tag and paste it anywhere in your message. Tags are checked again when sending.")
	buttons := [][]models.InlineKeyboardButton{}
	start, end := (page-1)*10, page*10
	if end > len(rows) {
		end = len(rows)
	}
	for _, row := range rows[start:end] {
		token := "@" + row.username
		if row.username == "" {
			token, err = p.mentions.Token(ctx, job, c.Route.ChatID, row.id)
			if err != nil {
				return err
			}
		}
		label := "…"
		if strings.HasSuffix(row.id, "@lid") {
			label = "ID …"
		}
		local := strings.Split(row.id, "@")[0]
		if len(local) > 6 {
			local = local[len(local)-6:]
		}
		value = presentation.Concat(value, presentation.Plain("\n"+row.name+" · "+label+local+"\n"), presentation.Styled(token, models.MessageEntityTypeCode))
		buttons = append(buttons, []models.InlineKeyboardButton{{Text: "Copy tag · " + row.name, CopyText: &models.CopyTextButton{Text: token}}})
	}
	if len(rows) == 0 {
		value = presentation.Concat(value, presentation.Plain("\nNo matching group members. Try /mention with a different name."))
	}
	navigation := []models.InlineKeyboardButton{}
	for _, next := range []struct {
		label string
		page  int
	}{{"Previous", page - 1}, {"Next", page + 1}} {
		if next.page < 1 || next.page > pages {
			continue
		}
		id := rand.Text()
		plan.Actions = append(plan.Actions, control.Action{ID: id, Kind: "mention", BotID: c.Target.BotID, WhatsAppChat: c.Route.ChatID, OwnerID: p.c.OwnerID, ChatID: c.Target.ChatID, ThreadID: c.Target.ThreadID, Search: search, Page: next.page, ExpiresAt: p.c.Now().Add(p.c.ControlTTL).UnixMilli()})
		navigation = append(navigation, models.InlineKeyboardButton{Text: next.label, CallbackData: "mention:" + id})
	}
	if len(navigation) > 0 {
		buttons = append(buttons, navigation)
	}
	parts, err := presentation.Split(value, 3800)
	if err != nil {
		return delivery.ErrInvalid
	}
	for _, part := range parts {
		plan.Packets = append(plan.Packets, control.Packet{Text: part.Text, Entities: part.Entities, Markup: &models.InlineKeyboardMarkup{InlineKeyboard: buttons}})
	}
	plan.Scope = &c
	plan.ReplyTTLSeconds = int(p.c.ControlTTL / time.Second)
	return nil
}
func (p *Picker) callback(ctx context.Context, job delivery.Job, e *delivery.Effects, botID string, q *models.CallbackQuery) error {
	if q.ID == "" {
		return delivery.ErrInvalid
	}
	plan := control.Plan{Key: job.Dedup, Target: control.Target{BotID: botID}, Answer: &bot.AnswerCallbackQueryParams{CallbackQueryID: q.ID}, ReplyTTLSeconds: int(p.c.CommandTTL / time.Second)}
	a, known, err := p.control.Action(ctx, job, strings.TrimPrefix(q.Data, "mention:"))
	if err != nil {
		return err
	}
	var c control.Context
	m := q.Message.Message
	if q.Message.Type == models.MaybeInaccessibleMessageTypeMessage && m != nil {
		c, err = p.control.Context(ctx, job, botID, m)
		if err != nil {
			return err
		}
		plan.Target = c.Target
	}
	valid := known && m != nil && activeGroup(c) && a.Kind == "mention" && a.OwnerID == p.c.OwnerID && a.BotID == botID && a.ChatID == c.Target.ChatID && a.ThreadID == c.Target.ThreadID && a.WhatsAppChat == c.Route.ChatID && a.ExpiresAt > p.c.Now().UnixMilli()
	if !valid {
		plan.Answer.Text = pickerExpired
	} else {
		if err := p.page(ctx, job, &plan, c, a.Search, a.Page); err != nil {
			return err
		}
	}
	return p.control.Send(ctx, job, e, plan)
}

// Refusal resumes an already frozen refusal before retrying a send, or records
// a definite mention-validation refusal. It never masks a provider/send error.
func (p *Picker) Refusal(ctx context.Context, job delivery.Job, e *delivery.Effects, botID string, m *models.Message, cause error) (bool, error) {
	if m == nil || m.From == nil || m.From.ID != p.c.OwnerID || m.From.IsBot {
		return false, nil
	}
	knownCause := errors.Is(cause, ErrTag) || errors.Is(cause, ErrDeparted) || errors.Is(cause, ErrRoster)
	if cause != nil && !knownCause {
		return false, nil
	}
	saved, known, err := p.control.Prepared(ctx, job, job.Dedup)
	if err != nil {
		return knownCause, err
	}
	if known && saved.Prefix == job.Dedup+":mention-error" {
		return true, p.control.Send(ctx, job, e, saved)
	}
	text := ""
	switch {
	case errors.Is(cause, ErrTag):
		text = "This mention tag is unknown or belongs to another group/account. Use /mention to copy a new tag, then resend."
	case errors.Is(cause, ErrDeparted):
		text = "A tagged person is no longer in this WhatsApp group. Use /mention to find a current member, then resend."
	case errors.Is(cause, ErrRoster):
		text = pickerRosterError
	default:
		return false, nil
	}
	c, err := p.control.Context(ctx, job, botID, m)
	if err != nil {
		return true, err
	}
	plan := control.Plan{Key: job.Dedup, Prefix: job.Dedup + ":mention-error", Target: c.Target, Packets: []control.Packet{{Text: text}}, ReplyTTLSeconds: int(p.c.CommandTTL / time.Second)}
	return true, p.control.Send(ctx, job, e, plan)
}
