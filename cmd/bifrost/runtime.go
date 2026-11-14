package main

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/veryshyjelly/OpenWA/sdk/go"
	"github.com/veryshyjelly/bifrost/internal/attachments"
	"github.com/veryshyjelly/bifrost/internal/config"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/httpserver"
	"github.com/veryshyjelly/bifrost/internal/identity"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/operations"
	"github.com/veryshyjelly/bifrost/internal/postgres"
	"github.com/veryshyjelly/bifrost/internal/providers/openwa"
	"github.com/veryshyjelly/bifrost/internal/statuses"
)

func runRuntime(ctx context.Context, c config.Runtime) error {
	call, finish := context.WithTimeout(ctx, 15*time.Second)
	store, err := postgres.Open(call, c.DatabaseURL)
	finish()
	if err != nil {
		return err
	}
	defer store.Close()
	if _, err := store.InitializeIdentity(ctx, c.AccountID, c.EncryptionKey); err != nil {
		return errors.New("runtime identity/key verification failed; migrate explicitly before startup")
	}
	listener, err := net.Listen("tcp", c.ListenAddress())
	if err != nil {
		return errors.New("cannot bind bridge HTTP port")
	}
	defer listener.Close()
	return serveRuntime(ctx, store, c, listener, nil)
}

// serveRuntime is the explicit dispatch boundary. Diagnostic serve never calls
// it. All shutdown paths join work before releasing account ownership.
func serveRuntime(ctx context.Context, store *postgres.Store, c config.Runtime, listener net.Listener, transport http.RoundTripper) error {
	if err := validateRuntimeIdentity(ctx, store, c); err != nil {
		return err
	}
	call, finish := context.WithTimeout(ctx, 10*time.Second)
	lease, err := store.AcquireRuntimeLease(call, c.AccountID)
	finish()
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := lease.Close(cleanup); err != nil {
			slog.Error("runtime ownership release failed")
		}
	}()
	input, err := ingress.New(ingress.Options{AccountID: c.AccountID, SessionID: c.OpenWASession, WebhookSecret: c.WebhookSecret, AdminToken: c.AdminToken, OwnerID: c.OwnerID, ManagementChatID: c.ManagementChatID, MasterBotID: c.MasterBotID}, store)
	if err != nil {
		return err
	}
	services, err := composeRuntimeServices(store, runtimeServiceOptions{
		Account: c.AccountID, Session: c.OpenWASession, OpenWAURL: c.OpenWAURL, OpenWAKey: c.OpenWAKey,
		TelegramURL: c.TelegramURL, CloudURL: c.CloudURL, MasterToken: c.MasterToken, Callback: c.Callback, AdminToken: c.AdminToken,
		MediaRoot: c.MediaRoot, TelegramRoot: c.TelegramRoot, Cron: c.SyncCron, Timezone: c.SyncTimezone,
		Owner: c.OwnerID, Management: c.ManagementChatID, MaxBytes: c.MaxBytes, HistoryLimit: c.HistoryLimit,
		CommandTTL: int(c.CommandTTL / time.Second), ControlTTL: int(c.ControlTTL / time.Second), Key: c.EncryptionKey,
		Codec: attachments.CodecOptions{Python: c.Python, Chromium: c.Chromium, NoSandbox: c.NoSandbox}, Transport: transport,
	})
	if err != nil {
		return err
	}
	wa, err := openwa.New(c.OpenWAURL, c.OpenWAKey, transport)
	if err != nil {
		return err
	}
	var ready atomic.Bool
	server := &http.Server{Handler: httpserver.Handler(store, c.AdminToken, httpserver.Options{Ingress: input, Ready: ready.Load}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	work, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var tasks sync.WaitGroup
	launch := func(task func(context.Context) error) {
		tasks.Go(func() {
			err := task(work)
			if work.Err() == nil {
				if err == nil {
					err = errors.New("runtime task stopped unexpectedly")
				}
				cancel(err)
			}
		})
	}
	launch(func(context.Context) error {
		if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			return errors.New("runtime HTTP server failed")
		}
		return nil
	})
	launch(func(ctx context.Context) error { return monitorRuntimeLease(ctx, lease) })
	startErr := func() error {
		startup, done := context.WithTimeout(work, 10*time.Minute)
		defer done()
		state, err := wa.Session(startup, c.OpenWASession)
		if err != nil || state.ID != c.OpenWASession || state.Status != "ready" {
			return errors.New("runtime requires the configured OpenWA session to be ready")
		}
		self, found, err := store.SelfIdentity(startup, c.AccountID)
		if err != nil {
			return errors.New("cannot verify saved WhatsApp identity")
		}
		if found {
			if state.Phone == nil {
				return errors.New("OpenWA did not provide its account phone identity")
			}
			phone, ok := identity.PhoneID(*state.Phone)
			if !ok || phone != self {
				return errors.New("OpenWA account differs from the retained WhatsApp identity")
			}
		}
		if err := services.bootstrapBots(startup, store, c.AccountID, c.MasterToken); err != nil {
			return err
		}
		if err := services.verifyBots(startup, store, c.AccountID, strings.TrimRight(c.Callback, "/"), c.AdminToken); err != nil {
			return err
		}
		events := []string{sdk.EventMessageReceived, sdk.EventMessageSent, sdk.EventMessageEdited, sdk.EventMessageRevoked, sdk.EventMessageReaction, sdk.EventPollVote, sdk.EventStatusReceived, sdk.EventSessionStatus, sdk.EventSessionDisconnected, sdk.EventMessageFailed}
		if err := wa.EnsureWebhook(startup, c.OpenWASession, strings.TrimRight(c.Callback, "/")+"/webhooks/openwa", c.WebhookSecret, events); err != nil {
			return errors.New("OpenWA callback setup failed")
		}
		if _, err := store.QueueStatusStartup(startup, c.AccountID, "status-startup:"+rand.Text(), statuses.Settings{Session: c.OpenWASession, OwnerID: c.OwnerID, Management: c.ManagementChatID, CommandTTL: int(c.CommandTTL / time.Second), ControlTTL: int(c.ControlTTL / time.Second), NoticeTTL: int(c.CommandTTL / time.Second)}); err != nil {
			return errors.New("cannot admit Status startup refresh")
		}
		if err := store.ConfigureOperationNotices(startup, c.AccountID, operations.Settings{Session: c.OpenWASession, SyncCron: c.SyncCron, SyncTimezone: c.SyncTimezone, OwnerID: c.OwnerID, Management: c.ManagementChatID, CommandTTL: int(c.CommandTTL / time.Second), ControlTTL: int(c.ControlTTL / time.Second), NoticeTTL: int(c.CommandTTL / time.Second), Configured: true}); err != nil {
			return errors.New("cannot enable operational notices")
		}
		paced := &runtimePacedStore{WorkerStore: store, interval: c.DeliveryInterval, now: time.Now}
		worker, err := delivery.NewWorker(paced, runtimeDiagnosticHandlers(services.Handlers, slog.Default()), delivery.WorkerOptions{Concurrency: 1, PollEvery: 100 * time.Millisecond, Report: func(r delivery.Report) {
			slog.Info("delivery state", "job", r.JobID, "state", r.State, "reason", r.Reason)
		}})
		if err != nil {
			return err
		}
		ready.Store(true)
		launch(worker.Run)
		launch(func(ctx context.Context) error { return services.Metadata.Schedule(ctx, c.AccountID, ready.Load) })
		slog.Info("dispatch runtime started", "port", c.Port, "configured", true)
		return nil
	}()
	if startErr != nil {
		cancel(startErr)
	} else {
		<-work.Done()
	}
	ready.Store(false)
	cancel(nil)
	shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
	shutdownErr := server.Shutdown(shutdown)
	done()
	if shutdownErr != nil {
		_ = server.Close()
	}
	tasks.Wait()
	if shutdownErr != nil {
		return errors.New("runtime HTTP shutdown deadline exceeded")
	}
	if ctx.Err() != nil {
		return nil
	}
	return context.Cause(work)
}

func validateRuntimeIdentity(ctx context.Context, store *postgres.Store, c config.Runtime) error {
	retained, found, err := store.RetainedRuntimeConfiguration(ctx, c.AccountID, c.EncryptionKey)
	if err != nil {
		return errors.New("cannot verify retained runtime configuration")
	}
	if !found {
		return nil
	}
	if retained.OwnerID() != c.OwnerID || retained.ManagementChatID() != c.ManagementChatID || retained.MasterID() != c.MasterBotID || retained.Value("TELEGRAM_MASTER_TOKEN") != c.MasterToken || retained.Value("BRIDGE_ADMIN_TOKEN") != c.AdminToken || retained.Value("OPENWA_WEBHOOK_SECRET") != c.WebhookSecret {
		return errors.New("runtime identity credentials differ from the retained account configuration")
	}
	return nil
}

func monitorRuntimeLease(ctx context.Context, lease *postgres.RuntimeLease) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			call, done := context.WithTimeout(ctx, 3*time.Second)
			err := lease.Check(call)
			done()
			if err != nil {
				return postgres.ErrRuntimeLeaseLost
			}
		}
	}
}
