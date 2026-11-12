package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/statuses"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

func statusFeed(ctx context.Context, q *db.Queries, account string, management int64) (statuses.Feed, bool, error) {
	r, err := q.StatusFeed(ctx, db.StatusFeedParams{AccountID: account, Management: management})
	if errors.Is(err, pgx.ErrNoRows) {
		return statuses.Feed{}, false, nil
	}
	if err != nil {
		return statuses.Feed{}, false, err
	}
	var topic statuses.Topic
	if json.Unmarshal(r.Payload, &topic) != nil {
		return topic.Feed, false, delivery.ErrInvalid
	}
	return topic.Feed, r.Complete, nil
}
func (s *Store) StatusTopic(ctx context.Context, account string, management int64) (statuses.Feed, bool, error) {
	return statusFeed(ctx, s.queries, account, management)
}
func (s *Store) StatusReaction(ctx context.Context, account, bot string, chat, message int64) (bool, error) {
	m, err := s.queries.TelegramMapping(ctx, db.TelegramMappingParams{AccountID: account, Scope: relayScope(bot, chat), TelegramChatID: chat, TelegramMessageID: message})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil && m.WhatsappChat == statuses.ChatID, err
}

func statusName(d *identity.Directory, m statuses.Mute) string {
	if name := d.SenderName(m.Author); name != "" && name != m.Author {
		return name
	}
	if m.Name != "" {
		return m.Name
	}
	return m.Author
}
func setStatusMute(ctx context.Context, q *db.Queries, row db.Job, d *identity.Directory, author, name string, active bool) (statuses.Mute, error) {
	if raw, err := q.StatusMuteOperation(ctx, db.StatusMuteOperationParams{AccountID: row.AccountID, JobID: row.ID}); err == nil {
		var result statuses.Mute
		if json.Unmarshal(raw, &result) != nil {
			return result, delivery.ErrInvalid
		}
		return result, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return statuses.Mute{}, err
	}
	author = d.Canonical(author)
	if !identity.IsPerson(author) {
		return statuses.Mute{}, delivery.ErrInvalid
	}
	revision, err := q.NextStatusMuteRevision(ctx, row.AccountID)
	if err != nil {
		return statuses.Mute{}, err
	}
	m := statuses.Mute{Author: author, Name: name, Active: active, Revision: revision}
	m.Name = statusName(d, m)
	raw, _ := json.Marshal(m)
	if err := q.SaveStatusMute(ctx, db.SaveStatusMuteParams{AccountID: row.AccountID, Author: author, Payload: raw}); err != nil {
		return m, err
	}
	return m, q.SaveStatusMuteOperation(ctx, db.SaveStatusMuteOperationParams{AccountID: row.AccountID, JobID: row.ID, Payload: raw})
}
func statusButton(ctx context.Context, q *db.Queries, row db.Job, proof string, c statuses.Settings, a statuses.Control, label string) (models.InlineKeyboardButton, error) {
	a.ID = groupToken()
	a.OwnerID = c.OwnerID
	a.ExpiresAt = time.Now().Add(time.Duration(c.ControlTTL) * time.Second).UnixMilli()
	raw, _ := json.Marshal(a)
	err := q.CreateStatusControl(ctx, db.CreateStatusControlParams{AccountID: row.AccountID, ControlID: a.ID, PlanKey: proof, Payload: raw})
	return models.InlineKeyboardButton{Text: label, CallbackData: "statusctl:" + a.ID}, err
}
func statusPage(ctx context.Context, q *db.Queries, row db.Job, d *identity.Directory, proof string, c statuses.Settings, target control.Target, page int, presentationJob int64) (presentation.Value, models.InlineKeyboardMarkup, error) {
	all, err := statusMutes(ctx, q, row.AccountID, d)
	if err != nil {
		return presentation.Value{}, models.InlineKeyboardMarkup{}, err
	}
	people := []statuses.Mute{}
	for _, m := range all {
		if m.Active {
			m.Name = statusName(d, m)
			people = append(people, m)
		}
	}
	collator := collate.New(language.AmericanEnglish)
	sort.Slice(people, func(i, j int) bool {
		n := collator.CompareString(people[i].Name, people[j].Name)
		if n == 0 {
			return people[i].Author < people[j].Author
		}
		return n < 0
	})
	pages := max(1, (len(people)+9)/10)
	page = max(1, min(page, pages))
	body := presentation.Plain("Nobody’s Status posts are muted.")
	if len(people) > 0 {
		noun := "people"
		if len(people) == 1 {
			noun = "person"
		}
		body = presentation.Plain(fmt.Sprintf("Muted Status posts (%d %s, page %d/%d):", len(people), noun, page, pages))
	}
	markup := models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}}
	for index := (page - 1) * 10; index < min(page*10, len(people)); index++ {
		m := people[index]
		label := []rune(m.Name)
		if len(label) > 50 {
			label = label[:50]
		}
		body = presentation.Concat(body, presentation.Plain(fmt.Sprintf("\n%d. ", index+1)), presentation.Sender(string(label), m.Author))
		if len(label) > 25 {
			label = label[:25]
		}
		button, err := statusButton(ctx, q, row, proof, c, statuses.Control{Kind: "unmute", Author: m.Author, Revision: m.Revision, View: "list", Page: page, ChatID: target.ChatID, ThreadID: target.ThreadID, PresentationJob: presentationJob}, "Unmute "+string(label))
		if err != nil {
			return body, markup, err
		}
		markup.InlineKeyboard = append(markup.InlineKeyboard, []models.InlineKeyboardButton{button})
	}
	nav := []models.InlineKeyboardButton{}
	for _, entry := range []struct {
		want bool
		page int
		text string
	}{{page > 1, page - 1, "Previous"}, {page < pages, page + 1, "Next"}} {
		if entry.want {
			button, err := statusButton(ctx, q, row, proof, c, statuses.Control{Kind: "page", Page: entry.page, ChatID: target.ChatID, ThreadID: target.ThreadID, PresentationJob: presentationJob}, entry.text)
			if err != nil {
				return body, markup, err
			}
			nav = append(nav, button)
		}
	}
	if len(nav) > 0 {
		markup.InlineKeyboard = append(markup.InlineKeyboard, nav)
	}
	return body, markup, nil
}

func (s *Store) PrepareStatusCommand(ctx context.Context, j delivery.Job, c statuses.Settings) (control.Plan, error) {
	var result control.Plan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if row.Kind != "tg" || !statuses.ValidSettings(c) {
			return delivery.ErrInvalid
		}
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if p, found, err := existingGroupPlan(ctx, q, row); err != nil {
			return err
		} else if found {
			result = p
			return nil
		}
		var envelope struct {
			Bot    string
			Update json.RawMessage
		}
		var u models.Update
		if json.Unmarshal(row.Payload, &envelope) != nil || envelope.Bot != "master" || json.Unmarshal(envelope.Update, &u) != nil || u.Message == nil || u.EditedMessage != nil || row.Dedup != "tg:master:"+strconv.FormatInt(u.ID, 10) || !ingress.AuthorizedUpdate(envelope.Update, &u, c.OwnerID) {
			return delivery.ErrInvalid
		}
		m := u.Message
		if m.From == nil || m.From.IsBot || m.From.ID != c.OwnerID || m.Chat.Type == models.ChatTypePrivate && m.Chat.ID != c.OwnerID {
			return delivery.ErrInvalid
		}
		target := control.Target{BotID: "master", ChatID: m.Chat.ID, ThreadID: int64(m.MessageThreadID)}
		feed, known, err := statusFeed(ctx, q, row.AccountID, c.Management)
		if err != nil {
			return err
		}
		inTopic := known && m.Chat.ID == feed.Management && target.ThreadID == feed.ThreadID
		if !inTopic {
			current, err := operatorContext(ctx, q, row.AccountID, "master", m, c.Management)
			if err != nil {
				return err
			}
			target = current.Target
		}
		parsed, isCommand := control.ParseCommand(m)
		p := control.Plan{Key: row.Dedup, Target: target, ReplyTTLSeconds: c.NoticeTTL}
		if isCommand {
			p.Command = &control.Command{MessageID: int64(m.ID), Date: int64(m.Date) * 1000, TTLSeconds: c.CommandTTL}
		}
		d, err := directory(ctx, q, row.AccountID)
		if err != nil {
			return err
		}
		switch {
		case isCommand && parsed.Name == "statuses":
			if _, err := metadataEnqueue(ctx, q, row.AccountID, row.Dedup+":status-refresh", "status-control:"+strconv.FormatInt(c.Management, 10), "status-refresh", map[string]any{"Session": c.Session, "Management": c.Management}); err != nil {
				return err
			}
			packet := control.Packet{Text: "Refreshing available WhatsApp Status posts. New posts appear in the Status topic automatically."}
			if known {
				url := "https://t.me/c/" + strings.TrimPrefix(strconv.FormatInt(feed.Management, 10), "-100") + "/" + strconv.FormatInt(feed.ThreadID, 10)
				packet.Markup = &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: "Open WhatsApp Status", URL: url}}}}
			}
			p.Packets = []control.Packet{packet}
		case isCommand && parsed.Name == "muted_statuses":
			page := 1
			valid := len(parsed.Args) <= 1
			if len(parsed.Args) == 1 {
				v := parsed.Args[0]
				n, err := strconv.Atoi(v)
				valid = err == nil && n > 0 && len(v) <= 6 && v[0] >= '1' && v[0] <= '9'
				if valid {
					for _, r := range v {
						if r < '0' || r > '9' {
							valid = false
						}
					}
				}
				page = n
			}
			if !valid {
				p.Packets = []control.Packet{{Text: "Use /muted_statuses or /muted_statuses <page>."}}
			} else {
				body, markup, err := statusPage(ctx, q, row, d, row.Dedup, c, target, page, 0)
				if err != nil {
					return err
				}
				p.Packets = []control.Packet{{Text: body.Text, Entities: body.Entities, Markup: &markup}}
			}
		case isCommand && (parsed.Name == "mute_status" || parsed.Name == "unmute_status" || inTopic && (parsed.Name == "mute" || parsed.Name == "unmute" || parsed.Name == "keep")):
			var author, name string
			if inTopic && len(parsed.Args) == 0 && m.ReplyToMessage != nil {
				mapping, err := q.TelegramMapping(ctx, db.TelegramMappingParams{AccountID: row.AccountID, Scope: "*", TelegramChatID: m.Chat.ID, TelegramMessageID: int64(m.ReplyToMessage.ID)})
				if err == nil && !mapping.Deleted && !mapping.ObservationDeleted && mapping.WhatsappChat == statuses.ChatID && mapping.ThreadID == target.ThreadID {
					post, err := q.StatusPost(ctx, db.StatusPostParams{AccountID: row.AccountID, PostID: mapping.WhatsappMessageID})
					if err != nil {
						return err
					}
					var source identity.Source
					if json.Unmarshal(post.Source, &source) != nil {
						return delivery.ErrInvalid
					}
					author, name = source.SenderID, source.Name
				} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
			}
			if author == "" {
				p.Packets = []control.Packet{{Text: "Reply to a post in WhatsApp Status with /mute_status or /unmute_status. Use /muted_statuses to manage muted people."}}
			} else {
				active := parsed.Name == "mute" || parsed.Name == "mute_status"
				mute, err := setStatusMute(ctx, q, row, d, author, name, active)
				if err != nil {
					return err
				}
				prefix, suffix := "Unmuted Status posts from ", ". New posts will appear again."
				if active {
					prefix, suffix = "Muted Status posts from ", ". Their chats continue as usual."
				}
				body := presentation.Concat(presentation.Plain(prefix), presentation.Sender(statusName(d, mute), mute.Author), presentation.Plain(suffix))
				packet := control.Packet{Text: body.Text, Entities: body.Entities}
				if active {
					button, err := statusButton(ctx, q, row, row.Dedup, c, statuses.Control{Kind: "unmute", Author: mute.Author, Revision: mute.Revision, View: "confirmation", ChatID: target.ChatID, ThreadID: target.ThreadID}, "Unmute")
					if err != nil {
						return err
					}
					packet.Markup = &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{button}}}
				}
				p.Packets = []control.Packet{packet}
			}
		case inTopic && !isCommand:
			p.Packets = []control.Packet{{Text: "This topic archives WhatsApp Status posts. Reply to a post with /mute to mute its author’s Status posts, or open the person’s chat to reply."}}
		default:
			return delivery.ErrUnsupported
		}
		if err := saveGroupPlan(ctx, q, row, p); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}
