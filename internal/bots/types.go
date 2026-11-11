// Package bots manages encrypted Telegram credentials and explicit local API setup.
package bots

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
)

type Credential struct {
	BotID, Ciphertext, Revision string
	Identity                    models.User
	CreatedNotice               bool
}
type SetupPlan struct {
	BotID, Revision, Configuration string
	Identity                       models.User
	Step                           int
}
type Settings struct {
	Session, Callback, Configuration string
	OwnerID, Management              int64
	CommandTTL, ControlTTL           int
}
type Store interface {
	Credential(context.Context, string, string) (Credential, bool, error)
	ManagedRegistration(context.Context, delivery.Job) (bool, error)
	RegisterManagedBot(context.Context, delivery.Job, Credential, Settings) error
	PrepareBotSetup(context.Context, delivery.Job, Settings) (SetupPlan, bool, error)
	AdvanceBotSetup(context.Context, delivery.Job, SetupPlan, Settings) error
	SeedMasterCredential(context.Context, string, Credential, Settings) error
	QueueBotSetups(context.Context, string, Settings) error
}
type Telegram interface {
	Me(context.Context) (*models.User, error)
	ManagedToken(context.Context, *bot.GetManagedBotTokenParams) (string, error)
	LogOut(context.Context) error
	SetWebhook(context.Context, *bot.SetWebhookParams) error
}
type Clients func(context.Context, string, string) (Telegram, error)

var tokenPattern = regexp.MustCompile(`^[1-9][0-9]*:[A-Za-z0-9_-]+$`)

func ValidToken(token string, id int64) bool {
	return tokenPattern.MatchString(token) && strings.HasPrefix(token, strconv.FormatInt(id, 10)+":")
}
func Digest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func ManagedIdentity(u *models.Update, owner int64) (models.User, bool) {
	if u == nil {
		return models.User{}, false
	}
	if m := u.ManagedBot; m != nil && m.User != nil && m.Bot != nil && m.User.ID == owner && !m.User.IsBot && m.Bot.IsBot && m.Bot.ID > 0 {
		return *m.Bot, true
	}
	if m := u.Message; m != nil && m.ManagedBotCreated != nil && m.ManagedBotCreated.Bot != nil && m.From != nil && m.From.ID == owner && !m.From.IsBot && m.ManagedBotCreated.Bot.IsBot && m.ManagedBotCreated.Bot.ID > 0 {
		return *m.ManagedBotCreated.Bot, true
	}
	return models.User{}, false
}
func local(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, delivery.ErrClaimLost) || errors.Is(err, delivery.ErrInvalid) || errors.Is(err, delivery.ErrUnsupported) || errors.Is(err, delivery.ErrUncertain) {
		return err
	}
	return delivery.ErrLocalRetry
}
