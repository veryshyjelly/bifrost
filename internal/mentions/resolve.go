package mentions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/presentation"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

var numericTag = regexp.MustCompile(`@[0-9]+`)
var usernameTag = regexp.MustCompile(`@[a-zA-Z0-9_]+`)
var copiedTag = regexp.MustCompile(`@\{[^{}\n]*(?:\}|$|\n)`)
var tokenKey = regexp.MustCompile(`\|([a-f0-9]{8,64})\}$`)

func word(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }
func boundaries(text string, start, end int, username bool) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(text[:start])
		if word(r) || r == '@' || username && r == '/' {
			return false
		}
	}
	if end < len(text) {
		r, _ := utf8.DecodeRuneInString(text[end:])
		if word(r) {
			return false
		}
	}
	return true
}
func localError(err error) error {
	if errors.Is(err, identity.ErrConflict) {
		return err
	}
	if err == nil || errors.Is(err, delivery.ErrClaimLost) {
		return err
	}
	return errors.Join(delivery.ErrLocalRetry, err)
}

// Incoming renders only numeric tags backed by explicit WhatsApp mention
// metadata. A numeric token with multiple possible people remains literal.
func (s *Service) Incoming(ctx context.Context, job delivery.Job, chat, body string, ids []string, destination int64) (presentation.Value, error) {
	if !utf8.ValidString(body) {
		return presentation.Value{}, delivery.ErrInvalid
	}
	d, err := s.store.IdentityDirectory(ctx, job.AccountID)
	if err != nil {
		return presentation.Value{}, localError(err)
	}
	roster, _, err := s.store.MentionRoster(ctx, job.AccountID, chat)
	if err != nil {
		return presentation.Value{}, localError(err)
	}
	targets := map[string]map[string]bool{}
	for _, id := range ids {
		if !identity.IsPerson(id) {
			continue
		}
		canonical := d.Canonical(id)
		for _, candidate := range append(d.Aliases(id), id) {
			n := identity.Local(candidate)
			if targets[n] == nil {
				targets[n] = map[string]bool{}
			}
			targets[n][canonical] = true
		}
	}
	result := presentation.Plain("")
	cursor := 0
	rendered := map[string]presentation.Value{}
	for _, span := range numericTag.FindAllStringIndex(body, -1) {
		start, end := span[0], span[1]
		if !boundaries(body, start, end, false) {
			continue
		}
		people := targets[body[start+1:end]]
		if len(people) != 1 {
			continue
		}
		var id string
		for candidate := range people {
			id = candidate
		}
		tag, ok := rendered[id]
		if !ok {
			p := d.Person(id, roster.Members)
			label := "@" + strings.TrimLeft(p.Name, "@")
			entity := models.MessageEntity{Type: models.MessageEntityTypeTextLink, Offset: 0, Length: presentation.Length(label), URL: presentation.WhatsAppURL(id)}
			member := false
			if p.Registered && destination < 0 {
				botID, err := strconv.ParseInt(p.Bot.ID, 10, 64)
				if err != nil || botID <= 0 {
					return presentation.Value{}, delivery.ErrInvalid
				}
				m, err := s.tg.ChatMember(ctx, &bot.GetChatMemberParams{ChatID: destination, UserID: botID})
				if err != nil {
					var failure *providers.Error
					if !errors.As(err, &failure) || failure.Service != "telegram" || failure.Status != 400 && failure.Status != 403 {
						return presentation.Value{}, err
					}
				} else {
					if m == nil {
						return presentation.Value{}, providers.InvalidResponse("telegram", false)
					}
					member = m.Type == models.ChatMemberTypeMember || m.Type == models.ChatMemberTypeAdministrator || m.Type == models.ChatMemberTypeOwner
				}
			}
			if member {
				botID, _ := strconv.ParseInt(p.Bot.ID, 10, 64)
				entity.Type = models.MessageEntityTypeTextMention
				entity.URL = ""
				entity.User = &models.User{ID: botID, IsBot: true, FirstName: identity.CleanName(p.Name), Username: p.Bot.Username}
			} else if p.Registered && p.Bot.Username != "" {
				entity.URL = "https://t.me/" + p.Bot.Username
			}
			tag = presentation.Value{Text: label, Entities: []models.MessageEntity{entity}}
			rendered[id] = tag
		}
		result = presentation.Concat(result, presentation.Plain(body[cursor:start]), tag)
		cursor = end
	}
	return presentation.Concat(result, presentation.Plain(body[cursor:])), nil
}

// byteOffsets maps valid UTF-16 entity boundaries to UTF-8 byte boundaries.
// A boundary in the middle of a surrogate pair is deliberately absent.
func byteOffsets(text string) map[int]int {
	result := map[int]int{}
	units := 0
	for i, r := range text {
		result[units] = i
		units++
		if r > 0xffff {
			units++
		}
	}
	result[units] = len(text)
	return result
}

type span struct {
	start, end int
	person     string
}

func (s *Service) Outgoing(ctx context.Context, job delivery.Job, chat, text string, entities []models.MessageEntity) (Outgoing, error) {
	if !utf8.ValidString(text) {
		return Outgoing{}, delivery.ErrInvalid
	}
	d, err := s.store.IdentityDirectory(ctx, job.AccountID)
	if err != nil {
		return Outgoing{}, localError(err)
	}
	indexes := byteOffsets(text)
	type interval struct{ start, end int }
	excluded := []interval{}
	for _, e := range entities {
		start, a := indexes[e.Offset]
		end, b := indexes[e.Offset+e.Length]
		if !a || !b || e.Offset < 0 || e.Length <= 0 || e.Length > presentation.Length(text) {
			return Outgoing{}, delivery.ErrInvalid
		}
		switch e.Type {
		case models.MessageEntityTypeCode, models.MessageEntityTypePre, models.MessageEntityTypeURL, models.MessageEntityTypeTextLink, models.MessageEntityTypeEmail:
			excluded = append(excluded, interval{start, end})
		}
	}
	blocked := func(start, end int) bool {
		for _, v := range excluded {
			if v.start < end && v.end > start {
				return true
			}
		}
		return false
	}
	spans := []span{}
	add := func(start, end int, id string) {
		if blocked(start, end) {
			return
		}
		for _, old := range spans {
			if old.start < end && old.end > start {
				return
			}
		}
		spans = append(spans, span{start, end, d.Canonical(id)})
	}
	for _, e := range entities {
		if e.Type == models.MessageEntityTypeTextMention && e.User != nil {
			if id, ok := d.ForBot(strconv.FormatInt(e.User.ID, 10)); ok {
				add(indexes[e.Offset], indexes[e.Offset+e.Length], id)
			}
		}
	}
	for _, v := range copiedTag.FindAllStringIndex(text, -1) {
		if blocked(v[0], v[1]) {
			continue
		}
		m := tokenKey.FindStringSubmatch(text[v[0]:v[1]])
		if m == nil {
			return Outgoing{}, ErrTag
		}
		token, ok, err := s.store.MentionToken(ctx, job.AccountID, m[1])
		if err != nil {
			return Outgoing{}, localError(err)
		}
		if !ok || token.ChatID != chat || !identity.IsPerson(token.PersonID) {
			return Outgoing{}, ErrTag
		}
		add(v[0], v[1], token.PersonID)
	}
	for _, v := range usernameTag.FindAllStringIndex(text, -1) {
		if boundaries(text, v[0], v[1], true) {
			if id, ok := d.ForUsername(text[v[0]+1 : v[1]]); ok {
				add(v[0], v[1], id)
			}
		}
	}
	if len(spans) > 0 && strings.HasSuffix(chat, "@g.us") {
		roster, err := s.Roster(ctx, job, chat, false)
		if err != nil {
			return Outgoing{}, err
		}
		present := map[string]bool{}
		for _, m := range roster.Members {
			present[d.Canonical(m.ID)] = true
		}
		for _, v := range spans {
			if !present[v.person] {
				return Outgoing{}, ErrDeparted
			}
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var converted strings.Builder
	cursor := 0
	ids := []string{}
	seen := map[string]bool{}
	for _, v := range spans {
		converted.WriteString(text[cursor:v.start])
		converted.WriteString("@" + identity.Local(v.person))
		cursor = v.end
		if !seen[v.person] {
			ids = append(ids, v.person)
			seen[v.person] = true
		}
	}
	converted.WriteString(text[cursor:])
	return Outgoing{Text: converted.String(), Mentions: ids}, nil
}

func digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Token uses the baseline account digest and collision-extension algorithm. The
// label is cosmetic: only the stored account/group/person tuple grants meaning.
func (s *Service) Token(ctx context.Context, job delivery.Job, chat, id string) (string, error) {
	if !strings.HasSuffix(chat, "@g.us") || !identity.IsPerson(id) {
		return "", delivery.ErrInvalid
	}
	d, err := s.store.IdentityDirectory(ctx, job.AccountID)
	if err != nil {
		return "", localError(err)
	}
	id = d.Canonical(id)
	roster, _, err := s.store.MentionRoster(ctx, job.AccountID, chat)
	if err != nil {
		return "", localError(err)
	}
	encoded, _ := json.Marshal([]string{digest(job.AccountID), chat, id})
	hash := digest(string(encoded))
	want := Token{ChatID: chat, PersonID: id}
	for n := 8; n <= 64; n += 2 {
		got, err := s.store.SaveMentionToken(ctx, job, hash[:n], want)
		if err != nil {
			return "", localError(err)
		}
		if got == want {
			return "@{" + d.Person(id, roster.Members).Name + "|" + hash[:n] + "}", nil
		}
	}
	return "", ErrTag
}
