package main

import (
	"context"
	"errors"
	"time"

	"github.com/go-telegram/bot"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres"
)

type runtimeBootstrapStore struct{ *postgres.Store }

func (s runtimeBootstrapStore) Claim(ctx context.Context, lease time.Duration) (delivery.Job, error) {
	return s.ClaimKinds(ctx, lease, []string{"control-bot-setup", "control-menu"})
}

// Recheck the actual local API on every restart. Prior journal receipts prove
// the migration decision, but cannot prove that today's endpoint is healthy.
func (s *runtimeServices) verifyBots(ctx context.Context, store *postgres.Store, account, callback, admin string) error {
	credentials, err := store.RuntimeCredentials(ctx, account)
	if err != nil || len(credentials) == 0 {
		return errors.New("cannot load runtime bot identities")
	}
	for _, credential := range credentials {
		client, err := s.Pool.Local(ctx, account, credential.BotID)
		if err != nil {
			return errors.New("cannot open runtime bot credential")
		}
		me, err := client.Me(ctx)
		if err != nil || me.ID != credential.Identity.ID || !me.IsBot {
			return errors.New("local Telegram identity verification failed")
		}
		if err := client.SetWebhook(ctx, &bot.SetWebhookParams{URL: callback + "/webhooks/telegram/" + credential.BotID, SecretToken: ingress.TelegramSecret(credential.BotID, admin), AllowedUpdates: []string{"message", "edited_message", "callback_query", "my_chat_member", "managed_bot", "message_reaction", "poll_answer"}, DropPendingUpdates: false}); err != nil {
			return errors.New("local Telegram callback refresh failed")
		}
	}
	return nil
}

// bootstrapBots admits and consumes only settings work. Ordinary authenticated
// ingress may be buffered, but it cannot overtake local token verification.
func (s *runtimeServices) bootstrapBots(ctx context.Context, store *postgres.Store, account, token string) error {
	if err := s.Bots.Bootstrap(ctx, account, token); err != nil {
		return err
	}
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	worker, err := delivery.NewWorker(runtimeBootstrapStore{store}, s.Handlers, delivery.WorkerOptions{Concurrency: 1, PollEvery: 100 * time.Millisecond})
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- worker.Run(work) }()
	stop := func() error { cancel(); return <-done }
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = stop()
			return ctx.Err()
		case err := <-done:
			if err != nil {
				return err
			}
			return errors.New("bot bootstrap stopped before readiness")
		case <-ticker.C:
			call, finish := context.WithTimeout(work, 5*time.Second)
			review, err := store.RuntimeBotSetupNeedsReview(call, account)
			if err == nil && review {
				err = errors.New("bot setup requires explicit review before runtime startup")
			}
			ready := false
			if err == nil {
				ready, err = store.RuntimeBotsReady(call, account, s.Configuration)
			}
			finish()
			if err != nil {
				_ = stop()
				return err
			}
			if ready {
				return stop()
			}
		}
	}
}
