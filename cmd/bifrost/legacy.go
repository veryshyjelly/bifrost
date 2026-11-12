package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/veryshyjelly/bifrost/internal/legacy"
)

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
