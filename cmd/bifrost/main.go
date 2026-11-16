// Command bifrost runs explicit migrations and the first diagnostic Go runtime.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/veryshyjelly/bifrost/internal/config"
	"github.com/veryshyjelly/bifrost/internal/httpserver"
	"github.com/veryshyjelly/bifrost/internal/ingress"
	"github.com/veryshyjelly/bifrost/internal/postgres"
)

// run selects an explicit operational command; the default is passive serve.
// Only runRuntime composes dispatch, so health and recovery tooling cannot
// accidentally start a second worker against the saved account.
func run(ctx context.Context, args []string) error {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}
	switch command {
	case "run":
		c, err := config.LoadRuntime(os.Getenv)
		if err != nil {
			return err
		}
		return runRuntime(ctx, c)
	case "review", "inspect-topic", "reconcile-topic":
		return recoveryCLI(ctx, command, args[1:], os.Stdout)
	case "check-telegram":
		return checkTelegram(ctx, args[1:], os.Stdout)
	case "check-openwa":
		return checkOpenWA(ctx, args[1:], os.Stdout)
	case "configure":
		if err := config.Configure(".go.env"); err != nil {
			if errors.Is(err, os.ErrExist) {
				fmt.Println("Existing .go.env preserved.")
				return nil
			}
			return errors.New("cannot create private .go.env")
		}
		fmt.Println("Created private .go.env. Fill TELEGRAM_API_ID and TELEGRAM_API_HASH, then run make up.")
		return nil
	case "healthcheck":
		port := os.Getenv("BRIDGE_PORT")
		if port == "" {
			port = "3210"
		}
		host := os.Getenv("BRIDGE_BIND_HOST")
		switch host {
		case "", "0.0.0.0":
			host = "127.0.0.1"
		case "::":
			host = "::1"
		}
		if net.ParseIP(host) == nil {
			return errors.New("BRIDGE_BIND_HOST must be an IP address")
		}
		client := http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
		if err != nil {
			return errors.New("bridge health check failed")
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return errors.New("bridge unhealthy")
		}
		return nil
	case "migrate":
		database, err := postgres.Open(ctx, os.Getenv("DATABASE_URL"))
		if err != nil {
			return err
		}
		defer database.Close()
		if err := database.Migrate(ctx); err != nil {
			return errors.New("database migration failed")
		}
		fmt.Println("PostgreSQL migrations applied.")
		return nil
	case "serve":
	default:
		return errors.New("use run, serve, migrate, configure, healthcheck, check-openwa, check-telegram, review, inspect-topic, reconcile-topic")
	}
	c, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	database, err := postgres.Open(connectCtx, c.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer database.Close()
	accountID, err := database.InitializeIdentity(ctx, c.AccountID, c.EncryptionKey)
	if err != nil {
		return errors.New("identity initialization failed; migrate explicitly and verify the saved identity/key")
	}
	var input http.Handler
	if c.IngressEnabled {
		input, err = ingress.New(ingress.Options{AccountID: accountID, SessionID: c.OpenWASession, WebhookSecret: c.WebhookSecret, AdminToken: c.AdminToken, OwnerID: c.OwnerID, ManagementChatID: c.ManagementChatID, MasterBotID: c.MasterBotID}, database)
		if err != nil {
			return err
		}
	}
	server := &http.Server{Addr: c.ListenAddress(), Handler: httpserver.Handler(database, c.AdminToken, httpserver.Options{Ingress: input}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return errors.New("cannot bind bridge HTTP port")
	}
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	slog.Info("diagnostic runtime started", "port", c.Port, "configured", false, "ingress", c.IngressEnabled)
	select {
	case err := <-result:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("HTTP server failed")
		}
		return nil
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return errors.New("HTTP shutdown deadline exceeded")
		}
		return nil
	}
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}
