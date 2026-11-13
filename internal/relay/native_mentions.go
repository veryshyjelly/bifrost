package relay

import (
	"context"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

// BotMentionLinkFallback preserves the frozen text and all other entities.
// Its caller must verify the saved username still belongs to the saved ID.
func BotMentionLinkFallback(packet NativePacket) (NativePacket, bool) {
	if packet.Kind != "text" || packet.Fallback != nil {
		return NativePacket{}, false
	}
	fallback := packet
	fallback.UseFallback, fallback.FallbackLabel = false, nil
	fallback.Body.Entities = append([]models.MessageEntity(nil), packet.Body.Entities...)
	changed := false
	for i, entity := range fallback.Body.Entities {
		u := entity.User
		if entity.Type != models.MessageEntityTypeTextMention || u == nil || !u.IsBot {
			continue
		}
		if u.ID <= 0 || u.Username == "" || len(u.Username) > 64 {
			return NativePacket{}, false
		}
		for _, c := range u.Username {
			if c != '_' && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
				return NativePacket{}, false
			}
		}
		entity.Type, entity.URL, entity.User = models.MessageEntityTypeTextLink, "https://t.me/"+u.Username, nil
		fallback.Body.Entities[i] = entity
		changed = true
	}
	return fallback, changed
}

type nativeMentionFallbackStore interface {
	PrepareNativeMentionFallback(context.Context, delivery.Job, string, int) (NativePlan, error)
}
type nativeMentionMembership interface {
	ChatMember(context.Context, *bot.GetChatMemberParams) (*models.ChatMember, error)
}

func nativeMentionUser(m *models.ChatMember) *models.User {
	if m == nil {
		return nil
	}
	switch m.Type {
	case models.ChatMemberTypeOwner:
		if m.Owner != nil {
			return m.Owner.User
		}
	case models.ChatMemberTypeAdministrator:
		if m.Administrator != nil {
			return &m.Administrator.User
		}
	case models.ChatMemberTypeMember:
		if m.Member != nil {
			return m.Member.User
		}
	case models.ChatMemberTypeRestricted:
		if m.Restricted != nil {
			return m.Restricted.User
		}
	case models.ChatMemberTypeLeft:
		if m.Left != nil {
			return m.Left.User
		}
	case models.ChatMemberTypeBanned:
		if m.Banned != nil {
			return m.Banned.User
		}
	}
	return nil
}

func (n *Native) mentionFallback(ctx context.Context, j delivery.Job, e *delivery.Effects, p NativePlan, part int) (NativePlan, bool, error) {
	store, ok := n.store.(nativeMentionFallbackStore)
	if !ok {
		return p, false, nil
	}
	packet := p.Packets[part]
	if _, ok := BotMentionLinkFallback(packet); !ok {
		return p, false, nil
	}
	client, err := n.clients(ctx, j.AccountID, "master")
	if err != nil {
		return p, false, local(err)
	}
	reader, ok := client.(nativeMentionMembership)
	if !ok {
		return p, false, delivery.ErrUnsupported
	}
	seen := map[int64]bool{}
	for _, entity := range packet.Body.Entities {
		u := entity.User
		if entity.Type != models.MessageEntityTypeTextMention || u == nil || !u.IsBot || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		if err := e.Permit(); err != nil {
			return p, false, err
		}
		member, err := reader.ChatMember(ctx, &bot.GetChatMemberParams{ChatID: packet.Target.ChatID, UserID: u.ID})
		if err != nil {
			return p, false, err
		}
		current := nativeMentionUser(member)
		if current == nil || !current.IsBot || current.ID != u.ID || !strings.EqualFold(current.Username, u.Username) {
			return p, false, delivery.ErrInvalid
		}
	}
	prepared, err := store.PrepareNativeMentionFallback(ctx, j, p.Key, part)
	return prepared, err == nil, local(err)
}
