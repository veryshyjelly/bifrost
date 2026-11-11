package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
)

var ErrTarget = errors.Join(delivery.ErrUnsupported, errors.New("metadata target is obsolete or not a private Telegram group"))

// Clip follows the original UTF-16 budget and removes unsupported controls.
// It never cuts a supplementary code point in half.
func Clip(text string, limit int) string {
	var b strings.Builder
	used := 0
	for _, r := range text {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			continue
		}
		n := utf16.RuneLen(r)
		if used+n > limit {
			break
		}
		b.WriteRune(r)
		used += n
	}
	return b.String()
}

func local(err error) error {
	if err == nil || errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, delivery.ErrInvalid) || errors.Is(err, delivery.ErrUnsupported) || errors.Is(err, identity.ErrConflict) {
		return err
	}
	var classified delivery.Outcome
	if errors.As(err, &classified) {
		return err
	}
	return delivery.ErrLocalRetry
}

func PrivateGroup(chat *models.ChatFullInfo) bool {
	return chat != nil && (chat.Type == models.ChatTypeGroup || chat.Type == models.ChatTypeSupergroup) && chat.Username == "" && len(chat.ActiveUsernames) == 0
}

// Prepare reads provider metadata only when there is no frozen plan. It does
// not mark a sync_metadata job complete; full refresh orchestration is separate.
func (s *Service) Prepare(ctx context.Context, j delivery.Job) (Plan, error) {
	if p, found, err := s.store.MetadataPlan(ctx, j); err != nil || found {
		return p, local(err)
	}
	target, err := s.store.MetadataTarget(ctx, j, s.session, s.management)
	if err != nil {
		return Plan{}, local(err)
	}
	snapshot := Snapshot{ID: target.ID, FetchedAt: s.now().UnixMilli()}
	if strings.HasSuffix(target.ID, "@g.us") {
		snapshot.Scope = "group"
		info, err := s.wa.GroupMetadata(ctx, s.session, target.ID)
		if err != nil {
			return Plan{}, err
		}
		if info == nil || info.ID != target.ID || strings.TrimSpace(info.Name) == "" || info.Participants == nil {
			return Plan{}, delivery.ErrInvalid
		}
		snapshot.Name = info.Name
		snapshot.Description = info.Description
		snapshot.Members = make([]identity.Member, 0, len(info.Participants))
		for _, m := range info.Participants {
			if !identity.IsPerson(m.ID) {
				return Plan{}, delivery.ErrInvalid
			}
			snapshot.Members = append(snapshot.Members, identity.Member{ID: m.ID, Name: m.Name, IsAdmin: m.IsAdmin, IsSuperAdmin: m.IsSuperAdmin})
		}
	} else {
		snapshot.Scope = "contact"
		info, err := s.wa.Contact(ctx, s.session, target.ID)
		if err != nil {
			return Plan{}, err
		}
		if info == nil || info.ID != target.ID || !identity.IsPerson(info.ID) {
			return Plan{}, delivery.ErrInvalid
		}
		for _, v := range []*string{info.Name, info.PushName} {
			if v != nil && strings.TrimSpace(*v) != "" {
				snapshot.Name = strings.TrimSpace(*v)
				break
			}
		}
		if snapshot.Name == "" {
			snapshot.Name = target.Name
		}
		if snapshot.Name == "" {
			snapshot.Name = target.ID
		}
	}
	changes := []Change{}
	warning := ""
	if route := target.Route; route != nil {
		if route.ChatID == s.management {
			if !route.Retired && route.ThreadID > 1 && Clip(route.Name, 128) != Clip(snapshot.Name, 128) {
				changes = append(changes, Change{Kind: "topic", BotID: "master", ChatID: route.ChatID, ThreadID: route.ThreadID, Text: Clip(snapshot.Name, 128)})
			}
		} else if snapshot.Scope == "group" {
			client, err := s.clients(ctx, j.AccountID, "master")
			if err != nil {
				return Plan{}, err
			}
			chat, err := client.Chat(ctx, &bot.GetChatParams{ChatID: route.ChatID})
			if err != nil {
				return Plan{}, err
			}
			if !PrivateGroup(chat) || chat.ID != route.ChatID {
				return Plan{}, ErrTarget
			}
			me, err := client.Me(ctx)
			if err != nil {
				return Plan{}, err
			}
			if me == nil || me.ID <= 0 {
				return Plan{}, delivery.ErrInvalid
			}
			member, err := client.ChatMember(ctx, &bot.GetChatMemberParams{ChatID: route.ChatID, UserID: me.ID})
			if err != nil {
				return Plan{}, err
			}
			if member != nil && member.Type == models.ChatMemberTypeAdministrator && member.Administrator != nil && member.Administrator.CanChangeInfo {
				if chat.Title != Clip(snapshot.Name, 128) {
					changes = append(changes, Change{Kind: "title", BotID: "master", ChatID: route.ChatID, Text: Clip(snapshot.Name, 128)})
				}
				if snapshot.Description != nil && chat.Description != Clip(*snapshot.Description, 255) {
					changes = append(changes, Change{Kind: "description", BotID: "master", ChatID: route.ChatID, Text: Clip(*snapshot.Description, 255)})
				}
			} else {
				warning = "Enable Change Group Info for Bifrost to synchronize this group’s title, description and picture."
			}
		}
	}
	if snapshot.Scope == "contact" && target.BotID != "" {
		client, err := s.clients(ctx, j.AccountID, target.BotID)
		if err != nil {
			return Plan{}, err
		}
		me, err := client.Me(ctx)
		if err != nil {
			return Plan{}, err
		}
		if me == nil || fmt.Sprint(me.ID) != target.BotID {
			return Plan{}, delivery.ErrInvalid
		}
		if me.FirstName != Clip(snapshot.Name, 64) {
			changes = append(changes, Change{Kind: "bot-name", BotID: target.BotID, Text: Clip(snapshot.Name, 64)})
		}
		current, err := client.MyDescription(ctx)
		if err != nil {
			return Plan{}, err
		}
		if current == nil {
			return Plan{}, delivery.ErrInvalid
		}
		description := Clip("WhatsApp bridge for "+snapshot.Name+". Replies are sent through your WhatsApp account.", 512)
		if current.Description != description {
			changes = append(changes, Change{Kind: "bot-description", BotID: target.BotID, Text: description})
		}
	}
	p, err := s.store.PrepareMetadata(ctx, j, target, snapshot, changes, warning)
	return p, local(err)
}

func (p Plan) ChangeKey(index int) string { return fmt.Sprintf("%s:metadata-text:%d", p.Key, index) }
func (p Plan) ChangeRequest(index int) json.RawMessage {
	b, _ := json.Marshal(struct {
		Session string
		Target  Target
		Change  Change
	}{p.Target.Session, p.Target, p.Changes[index]})
	return b
}

// ApplyText can be resumed independently of later photo/permission workflows.
// Setters retain frozen values and receipts. Every part checks the current
// account route, return generation, binding and metadata snapshot before use.
func (s *Service) ApplyText(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	return s.applyText(ctx, j, e, false)
}
func (s *Service) ApplyTextManaged(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	return s.applyText(ctx, j, e, true)
}
func (s *Service) applyText(ctx context.Context, j delivery.Job, e *delivery.Effects, managed bool) error {
	p, found, err := s.store.MetadataPlan(ctx, j)
	if err != nil {
		return local(err)
	}
	if !found || p.JobID != j.ID || p.Target.Session != s.session || p.Target.Management != s.management {
		return delivery.ErrInvalid
	}
	if p.TextComplete {
		return nil
	}
	for i, change := range p.Changes {
		if p.TextRefusals[i] != "" {
			continue
		}
		current, err := s.store.MetadataTextCurrent(ctx, j)
		if err != nil {
			return local(err)
		}
		if !current {
			return ErrTarget
		}
		if err := e.Permit(); err != nil {
			return err
		}
		client, err := s.clients(ctx, j.AccountID, change.BotID)
		if err != nil {
			return err
		}
		if change.Kind == "title" || change.Kind == "description" {
			chat, err := client.Chat(ctx, &bot.GetChatParams{ChatID: change.ChatID})
			if err != nil {
				return err
			}
			if !PrivateGroup(chat) || chat.ID != change.ChatID {
				return ErrTarget
			}
		}
		_, err = e.Do(p.ChangeKey(i), p.ChangeRequest(i), func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var request struct {
				Session string
				Target  Target
				Change  Change
			}
			if json.Unmarshal(raw, &request) != nil {
				return nil, delivery.ErrInvalid
			}
			c := request.Change
			switch c.Kind {
			case "topic":
				err = client.EditForumTopic(ctx, &bot.EditForumTopicParams{ChatID: c.ChatID, MessageThreadID: int(c.ThreadID), Name: c.Text})
			case "title":
				err = client.SetChatTitle(ctx, &bot.SetChatTitleParams{ChatID: c.ChatID, Title: c.Text})
			case "description":
				err = client.SetChatDescription(ctx, &bot.SetChatDescriptionParams{ChatID: c.ChatID, Description: c.Text})
			case "bot-name":
				err = client.SetMyName(ctx, &bot.SetMyNameParams{Name: c.Text})
			case "bot-description":
				err = client.SetMyDescription(ctx, &bot.SetMyDescriptionParams{Description: c.Text})
			default:
				return nil, delivery.ErrInvalid
			}
			if err != nil {
				return nil, err
			}
			return json.RawMessage(`true`), nil
		})
		if err != nil {
			if managed && definiteMetadataRefusal(err) {
				if saveErr := s.store.RefuseMetadataText(ctx, j, i, err.Error()); saveErr != nil {
					return local(saveErr)
				}
				continue
			}
			return err
		}
	}
	return local(s.store.CompleteMetadataText(ctx, j))
}
