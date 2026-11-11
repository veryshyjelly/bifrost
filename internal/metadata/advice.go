package metadata

import (
	"context"
	"errors"
	"net/url"
	"strconv"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/control"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

const ChangeInfoWarning = "Enable Change Group Info for Bifrost to synchronize this group’s title, description and picture."

type BotAdvice struct{ PersonID, ID, Username, Name string }
type Inspection struct {
	AccountID, Session string
	JobID, OwnerID     int64
	Target             Target
	Missing            []BotAdvice
	Candidates         []BotAdvice
	Permission         *control.SyncPermission
	PermissionLink     string
	PermissionNeeded   bool
	MasterUsername     string
}
type AdviceContext struct {
	Plan             Plan
	Candidates       []BotAdvice
	PermissionNeeded bool
}
type AdviceStore interface {
	MetadataInspection(context.Context, delivery.Job) (Inspection, bool, error)
	MetadataAdviceContext(context.Context, delivery.Job) (AdviceContext, error)
	PrepareMetadataInspection(context.Context, delivery.Job, Inspection, control.SyncSettings) (Inspection, error)
}
type AdviceTelegram interface {
	Me(context.Context) (*models.User, error)
	Chat(context.Context, *bot.GetChatParams) (*models.ChatFullInfo, error)
	ChatMember(context.Context, *bot.GetChatMemberParams) (*models.ChatMember, error)
}
type Inspector struct {
	store    AdviceStore
	master   AdviceTelegram
	settings control.SyncSettings
}

func NewInspector(store AdviceStore, master AdviceTelegram, c control.SyncSettings) (*Inspector, error) {
	if c.CommandTTL == 0 {
		c.CommandTTL = 60
	}
	if c.ControlTTL == 0 {
		c.ControlTTL = 600
	}
	if store == nil || master == nil || c.OwnerID <= 0 || c.Session == "" || c.Management >= 0 || c.CommandTTL < 5 || c.CommandTTL > 86400 || c.ControlTTL < 5 || c.ControlTTL > 86400 {
		return nil, delivery.ErrInvalid
	}
	return &Inspector{store: store, master: master, settings: c}, nil
}
func PermissionLink(username, id string) string {
	return "https://t.me/" + url.PathEscape(username) + "?startgroup=sync_" + id + "&admin=change_info+delete_messages+invite_users"
}

func (s *Inspector) Inspect(ctx context.Context, j delivery.Job) (Inspection, error) {
	if saved, found, err := s.store.MetadataInspection(ctx, j); err != nil || found {
		if found && (saved.OwnerID != s.settings.OwnerID || saved.Session != s.settings.Session) {
			return Inspection{}, delivery.ErrInvalid
		}
		return saved, local(err)
	}
	context, err := s.store.MetadataAdviceContext(ctx, j)
	if err != nil {
		return Inspection{}, local(err)
	}
	p := context.Plan
	if p.Target.Session != s.settings.Session || p.Target.Management != s.settings.Management {
		return Inspection{}, delivery.ErrInvalid
	}
	result := Inspection{AccountID: j.AccountID, Session: p.Target.Session, JobID: j.ID, OwnerID: s.settings.OwnerID, Target: p.Target, Candidates: append([]BotAdvice(nil), context.Candidates...)}
	if p.Snapshot.Scope == "group" && p.Target.Route != nil && p.Target.Route.ChatID != s.settings.Management {
		chat, err := s.master.Chat(ctx, &bot.GetChatParams{ChatID: p.Target.Route.ChatID})
		if err != nil {
			return result, err
		}
		if !PrivateGroup(chat) || chat.ID != p.Target.Route.ChatID {
			return result, ErrTarget
		}
		me, err := s.master.Me(ctx)
		if err != nil {
			return result, err
		}
		if me == nil || me.ID <= 0 || !me.IsBot {
			return result, delivery.ErrInvalid
		}
		member, err := s.master.ChatMember(ctx, &bot.GetChatMemberParams{ChatID: chat.ID, UserID: me.ID})
		if err != nil {
			return result, err
		}
		if member == nil || membershipUserID(member) != me.ID {
			return result, providers.InvalidResponse("telegram", false)
		}
		// The warning/grant reflects this native check, not stale text-stage rights.
		if context.PermissionNeeded || member.Type != models.ChatMemberTypeAdministrator || member.Administrator == nil || !member.Administrator.CanChangeInfo {
			if me.Username == "" {
				return result, delivery.ErrInvalid
			}
			result.PermissionNeeded = true
			result.MasterUsername = me.Username
		}
		for _, candidate := range context.Candidates {
			id, err := strconv.ParseInt(candidate.ID, 10, 64)
			if err != nil || id <= 0 {
				return result, delivery.ErrInvalid
			}
			membership, err := s.master.ChatMember(ctx, &bot.GetChatMemberParams{ChatID: chat.ID, UserID: id})
			if err != nil {
				var refusal *providers.Error
				if !errors.As(err, &refusal) || refusal.Status != 400 {
					return result, err
				}
				result.Missing = append(result.Missing, candidate)
				continue
			}
			if membership == nil || membershipUserID(membership) != id {
				return result, providers.InvalidResponse("telegram", false)
			}
			present := membership.Type == models.ChatMemberTypeMember || membership.Type == models.ChatMemberTypeAdministrator || membership.Type == models.ChatMemberTypeOwner
			if !present {
				result.Missing = append(result.Missing, candidate)
			}
		}
	}
	result, err = s.store.PrepareMetadataInspection(ctx, j, result, s.settings)
	return result, local(err)
}

func membershipUserID(m *models.ChatMember) int64 {
	switch m.Type {
	case models.ChatMemberTypeOwner:
		if m.Owner != nil {
			return m.Owner.User.ID
		}
	case models.ChatMemberTypeAdministrator:
		if m.Administrator != nil {
			return m.Administrator.User.ID
		}
	case models.ChatMemberTypeMember:
		if m.Member != nil {
			return m.Member.User.ID
		}
	case models.ChatMemberTypeRestricted:
		if m.Restricted != nil {
			return m.Restricted.User.ID
		}
	case models.ChatMemberTypeLeft:
		if m.Left != nil {
			return m.Left.User.ID
		}
	case models.ChatMemberTypeBanned:
		if m.Banned != nil {
			return m.Banned.User.ID
		}
	}
	return 0
}
