package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/veryshyjelly/bifrost/internal/config"
	"github.com/veryshyjelly/bifrost/internal/legacy"
	"github.com/veryshyjelly/bifrost/internal/postgres"
)

func activateLegacyCLI(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("legacy-activate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	file := flags.String("file", "", "frozen legacy archive")
	scratch := flags.String("scratch", "", "private temporary staging parent")
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		_, err := fmt.Fprintln(out, "bifrost legacy-activate --file private-export.tar.gz [--scratch DIRECTORY]; requires restored runtime environment and stopped media/Telegram writers; verifies assets and the fresh queue before releasing the import gate; starts no runtime")
		return err
	} else if err != nil || flags.NArg() != 0 || *file == "" {
		return errors.New("use legacy-activate --file private-export.tar.gz")
	}
	c, err := config.LoadRuntime(os.Getenv)
	if err != nil {
		return err
	}
	bundle, err := legacy.OpenBundle(ctx, *file, legacy.BundleOptions{Scratch: *scratch})
	if err != nil {
		return err
	}
	defer bundle.Close()
	if bundle.Account != c.AccountID || bundle.Configuration().EncryptionKey() != c.EncryptionKey {
		return errors.New("activation requires the original permanent account and key")
	}
	receipt, err := legacy.VerifyRestoredAssets(ctx, bundle, c.MediaRoot, c.TelegramRoot)
	if err != nil {
		return err
	}
	connect, cancel := context.WithTimeout(ctx, 15*time.Second)
	store, err := postgres.Open(connect, c.DatabaseURL)
	cancel()
	if err != nil {
		return errors.New("cannot connect to legacy activation destination")
	}
	defer store.Close()
	if err = validateRuntimeIdentity(ctx, store, c); err != nil {
		return err
	}
	if err = store.ActivateLegacyImport(ctx, bundle, receipt); err != nil {
		return errors.New("legacy activation refused; verify preparation, fresh queue and restored assets")
	}
	return json.NewEncoder(out).Encode(struct {
		Account       string `json:"account"`
		ArchiveDigest string `json:"archive_digest"`
		ImportReady   bool   `json:"import_ready"`
		Dispatching   bool   `json:"dispatching"`
	}{bundle.Account, bundle.Digest, true, false})
}

func prepareLegacyCLI(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("legacy-prepare", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	file := flags.String("file", "", "frozen legacy archive")
	scratch := flags.String("scratch", "", "private temporary staging parent")
	session := flags.String("session", "", "planned OpenWA connection (defaults to original)")
	freshQueue := flags.Bool("fresh-queue", false, "leave legacy jobs/history runs archived and start the Go queue empty")
	compressed := flags.Int64("max-archive-bytes", 0, "archive byte limit")
	expanded := flags.Int64("max-expanded-bytes", 0, "expanded byte limit")
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		_, err := fmt.Fprintln(out, "bifrost legacy-prepare --file private-export.tar.gz [--fresh-queue] [--session ID] [--scratch DIRECTORY] [--max-archive-bytes N] [--max-expanded-bytes N]; requires DATABASE_URL and an explicitly migrated empty destination; never activates delivery")
		return err
	} else if err != nil || flags.NArg() != 0 || *file == "" {
		return errors.New("use legacy-prepare --file private-export.tar.gz")
	}
	if os.Getenv("DATABASE_URL") == "" {
		return errors.New("legacy preparation requires DATABASE_URL")
	}
	bundle, err := legacy.OpenBundle(ctx, *file, legacy.BundleOptions{Scratch: *scratch, MaxArchiveBytes: *compressed, MaxExpandedBytes: *expanded})
	if err != nil {
		return err
	}
	defer bundle.Close()
	connect, cancel := context.WithTimeout(ctx, 15*time.Second)
	database, err := postgres.Open(connect, os.Getenv("DATABASE_URL"))
	cancel()
	if err != nil {
		return errors.New("cannot connect to legacy preparation destination")
	}
	defer database.Close()
	result, err := database.StageLegacyImport(ctx, bundle, legacy.SQLiteOptions{}, *session)
	if err != nil {
		var stage *postgres.LegacyStageError
		if errors.As(err, &stage) {
			return errors.New(stage.Error())
		}
		return errors.New("legacy preparation failed; delivery remains inactive")
	}
	if *freshQueue {
		if err := database.ChooseLegacyFreshQueue(ctx, bundle.Account, bundle.Digest); err != nil {
			return errors.New("cannot record fresh-queue cutover policy; delivery remains inactive")
		}
		result.QueueMode = "fresh"
	}
	return json.NewEncoder(out).Encode(result)
}

func validateLegacyCLI(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("legacy-validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	file := flags.String("file", "", "legacy export archive")
	scratch := flags.String("scratch", "", "private temporary staging parent")
	compressed := flags.Int64("max-archive-bytes", 0, "archive byte limit (zero uses default)")
	expanded := flags.Int64("max-expanded-bytes", 0, "expanded byte limit (zero uses default)")
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		_, err := fmt.Fprintln(out, "bifrost legacy-validate --file private-export.tar.gz [--scratch DIRECTORY] [--max-archive-bytes N] [--max-expanded-bytes N]")
		return err
	} else if err != nil || flags.NArg() != 0 || *file == "" {
		return errors.New("use legacy-validate --file private-export.tar.gz")
	}
	bundle, err := legacy.OpenBundle(ctx, *file, legacy.BundleOptions{Scratch: *scratch, MaxArchiveBytes: *compressed, MaxExpandedBytes: *expanded})
	if err != nil {
		return err
	}
	defer bundle.Close()
	report, err := legacy.ReadSnapshot(ctx, bundle, legacy.SQLiteOptions{}, nil)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(struct {
		Account       string                `json:"account"`
		ArchiveDigest string                `json:"archive_digest"`
		State         legacy.SnapshotReport `json:"state"`
	}{bundle.Account, bundle.Digest, report})
}
