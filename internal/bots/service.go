package bots

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/providers"
	"github.com/veryshyjelly/bifrost/internal/secrets"
)

type Options struct {
	Settings
	AdminToken string
	Key        [32]byte
}
type Service struct {
	store        Store
	local, cloud Clients
	cipher       *secrets.Cipher
	c            Options
}

// Bootstrap admits settings work only. The caller must initialize/verify the
// permanent database identity first and explicitly start a configured worker.
func (s *Service) Bootstrap(ctx context.Context, account, token string) error {
	id, err := strconv.ParseInt(strings.Split(token, ":")[0], 10, 64)
	if err != nil || !ValidToken(token, id) || account == "" {
		return delivery.ErrInvalid
	}
	sealed, err := s.cipher.Seal(token)
	if err != nil {
		return delivery.ErrLocalRetry
	}
	c := Credential{BotID: "master", Ciphertext: sealed, Revision: Digest(token), Identity: models.User{ID: id, IsBot: true}, CreatedNotice: true}
	if err := s.store.SeedMasterCredential(ctx, account, c, s.c.Settings); err != nil {
		return local(err)
	}
	return local(s.store.QueueBotSetups(ctx, account, s.c.Settings))
}

func New(store Store, localClients, cloudClients Clients, c Options) (*Service, error) {
	u, err := url.Parse(c.Callback)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || store == nil || localClients == nil || cloudClients == nil || c.OwnerID <= 0 || c.Management >= 0 || c.Session == "" || len(c.AdminToken) < 32 {
		return nil, delivery.ErrInvalid
	}
	if c.CommandTTL == 0 {
		c.CommandTTL = 60
	}
	if c.ControlTTL == 0 {
		c.ControlTTL = 600
	}
	if c.CommandTTL < 5 || c.CommandTTL > 86400 || c.ControlTTL < 5 || c.ControlTTL > 86400 {
		return nil, delivery.ErrInvalid
	}
	c.Callback = strings.TrimRight(c.Callback, "/")
	c.Configuration = Digest([]string{c.Callback, ingress.TelegramSecret("master", c.AdminToken)})
	cipher, err := secrets.New(c.Key)
	if err != nil {
		return nil, err
	}
	return &Service{store: store, local: localClients, cloud: cloudClients, cipher: cipher, c: c}, nil
}
func (s *Service) Handle(ctx context.Context, j delivery.Job, e *delivery.Effects, botID string, u *models.Update) error {
	if botID != "master" || (u != nil && u.ManagedBot == nil && (u.Message == nil || u.Message.ManagedBotCreated == nil)) {
		return delivery.ErrUnsupported
	}
	identity, ok := ManagedIdentity(u, s.c.OwnerID)
	if !ok {
		return nil
	}
	if identity.Username == "" || j.Kind != "tg" || e == nil {
		return delivery.ErrInvalid
	}
	if done, err := s.store.ManagedRegistration(ctx, j); err != nil || done {
		return local(err)
	}
	if err := e.Permit(); err != nil {
		return err
	}
	master, err := s.local(ctx, j.AccountID, "master")
	if err != nil {
		return err
	}
	token, err := master.ManagedToken(ctx, &bot.GetManagedBotTokenParams{UserID: identity.ID})
	if err != nil {
		return err
	}
	if !ValidToken(token, identity.ID) {
		return providers.InvalidResponse("telegram", false)
	}
	sealed, err := s.cipher.Seal(token)
	if err != nil {
		return delivery.ErrLocalRetry
	}
	return local(s.store.RegisterManagedBot(ctx, j, Credential{BotID: strconv.FormatInt(identity.ID, 10), Identity: identity, Ciphertext: sealed, Revision: Digest(token)}, s.c.Settings))
}
func (s *Service) Setup(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	if j.Kind != "control-bot-setup" || e == nil {
		return delivery.ErrInvalid
	}
	p, current, err := s.store.PrepareBotSetup(ctx, j, s.c.Settings)
	if err != nil {
		return local(err)
	}
	if !current {
		return nil
	}
	for p.Step < 3 {
		if err := e.Permit(); err != nil {
			return err
		}
		switch p.Step {
		case 0:
			cloud, err := s.cloud(ctx, j.AccountID, p.BotID)
			if err != nil {
				return err
			}
			request, _ := json.Marshal(struct{ Bot, Revision string }{p.BotID, p.Revision})
			if _, err = e.Do(j.Dedup+":cloud-logout", request, func(call context.Context, _ json.RawMessage) (json.RawMessage, error) {
				if err := cloud.LogOut(call); err != nil {
					return nil, err
				}
				return json.RawMessage(`true`), nil
			}); err != nil {
				return err
			}
		case 1:
			client, err := s.local(ctx, j.AccountID, p.BotID)
			if err != nil {
				return err
			}
			identity, err := client.Me(ctx)
			if err != nil {
				return err
			}
			if identity == nil || !identity.IsBot || identity.ID != p.Identity.ID || identity.Username == "" {
				return providers.InvalidResponse("telegram", false)
			}
			p.Identity = *identity
		case 2:
			client, err := s.local(ctx, j.AccountID, p.BotID)
			if err != nil {
				return err
			}
			if err = client.SetWebhook(ctx, &bot.SetWebhookParams{URL: s.c.Callback + "/webhooks/telegram/" + p.BotID, SecretToken: ingress.TelegramSecret(p.BotID, s.c.AdminToken), AllowedUpdates: []string{"message", "edited_message", "callback_query", "my_chat_member", "managed_bot", "message_reaction", "poll_answer"}, DropPendingUpdates: false}); err != nil {
				return err
			}
		}
		if err = s.store.AdvanceBotSetup(ctx, j, p, s.c.Settings); err != nil {
			return local(err)
		}
		p.Step++
	}
	return nil
}
func (s *Service) Handlers() map[string]delivery.Handler {
	return map[string]delivery.Handler{"control-bot-setup": s.Setup}
}
