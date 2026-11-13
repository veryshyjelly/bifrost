package main

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/bridge"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/postgres"
)

type forbiddenRuntimeTransport struct{ calls int }

func (t *forbiddenRuntimeTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, errors.New("construction contacted a provider")
}

func TestRuntimeCompositionIncludesEveryWorkflowWithoutStartingProviders(t *testing.T) {
	transport := &forbiddenRuntimeTransport{}
	c := runtimeServiceOptions{Account: "fixture-account", Session: "fixture-session", OpenWAURL: "http://openwa.invalid", OpenWAKey: "fixture-key", TelegramURL: "http://telegram.invalid", CloudURL: "https://cloud.invalid", MasterToken: "999:fixture-token", Callback: "http://bifrost.invalid:3210", AdminToken: "01234567890123456789012345678901", MediaRoot: t.TempDir(), TelegramRoot: t.TempDir(), Cron: "0 * * * *", Timezone: "UTC", Owner: 123, Management: -100, MaxBytes: 32 << 20, CommandTTL: 60, ControlTTL: 600, Transport: transport}
	// A zero store intentionally has no connection: composition may not query
	// credentials or launch setup/consumption before the lifecycle is started.
	s, err := composeRuntimeServices(&postgres.Store{}, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"wa", "tg", "management", "history", "history-message", "history-finish", "sync-request", "sync-start", "sync-target", "sync-seal", "control-sync-report", "status-refresh", "status-startup", "control-status-edit", "control-bot-setup", "control-bot-created", "control-menu", "control-retire-inbox", "control-notice", "control-source-action", "control-poll-vote", "control-revoke", "control-clear", "control-expire"} {
		if s.Handlers[kind] == nil {
			t.Fatal("missing runtime workflow", kind)
		}
	}
	if transport.calls != 0 {
		t.Fatal("composition performed provider requests")
	}
	if s.Bots == nil || s.Metadata == nil || s.Pool == nil {
		t.Fatal("missing lifecycle dependencies")
	}
}

func TestRuntimeHandlerChainsStopAtHandledOrFailedWork(t *testing.T) {
	for _, terminal := range []error{nil, delivery.ErrInvalid, delivery.ErrLocalRetry} {
		called := 0
		unsupported := func(context.Context, delivery.Job, *delivery.Effects, string, *models.Update) error {
			called++
			return delivery.ErrUnsupported
		}
		last := func(context.Context, delivery.Job, *delivery.Effects, string, *models.Update) error {
			called++
			return terminal
		}
		never := func(context.Context, delivery.Job, *delivery.Effects, string, *models.Update) error {
			t.Fatal("handled update reached a second workflow")
			return nil
		}
		err := runtimeTelegramChain(bridge.TelegramHandler(unsupported), last, never)(t.Context(), delivery.Job{}, nil, "master", nil)
		if !errors.Is(err, terminal) || called != 2 {
			t.Fatal("Telegram chain changed handler outcome")
		}
		called = 0
		wrap := func(h bridge.TelegramHandler) delivery.Handler {
			return func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
				return h(ctx, j, e, "master", nil)
			}
		}
		err = runtimeDeliveryChain(wrap(unsupported), wrap(last), wrap(never))(t.Context(), delivery.Job{}, nil)
		if !errors.Is(err, terminal) || called != 2 {
			t.Fatal("management chain changed handler outcome")
		}
	}
}
