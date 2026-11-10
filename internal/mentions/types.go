// Package mentions resolves explicit tags against verified identities and group
// membership before a relay plan freezes the text and native provider tags.
package mentions

import (
	"context"
	"errors"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/identity"
)

var (
	ErrTag      = errors.Join(delivery.ErrUnsupported, errors.New("mention tag is unknown or belongs to another group/account"))
	ErrRoster   = errors.Join(delivery.ErrUnsupported, errors.New("could not validate the WhatsApp member list"))
	ErrDeparted = errors.Join(delivery.ErrUnsupported, errors.New("a tagged person is no longer in this WhatsApp group"))
)

type Roster struct {
	ChatID, Name string
	Members      []identity.Member
	FetchedAt    int64
	Cached       bool
}
type Token struct{ ChatID, PersonID string }
type Outgoing struct {
	Text     string
	Mentions []string
}
type Store interface {
	IdentityDirectory(context.Context, string) (*identity.Directory, error)
	MentionRoster(context.Context, string, string) (Roster, bool, error)
	SaveMentionRoster(context.Context, delivery.Job, Roster) error
	MentionToken(context.Context, string, string) (Token, bool, error)
	SaveMentionToken(context.Context, delivery.Job, string, Token) (Token, error)
}
type WhatsApp interface {
	Group(context.Context, string, string) (*sdk.GroupInfo, error)
}
type Telegram interface {
	ChatMember(context.Context, *bot.GetChatMemberParams) (*models.ChatMember, error)
}
type Service struct {
	store   Store
	wa      WhatsApp
	tg      Telegram
	session string
	now     func() time.Time
}

func New(store Store, wa WhatsApp, tg Telegram, session string) (*Service, error) {
	if store == nil || wa == nil || tg == nil || session == "" {
		return nil, delivery.ErrInvalid
	}
	return &Service{store: store, wa: wa, tg: tg, session: session, now: time.Now}, nil
}
