package postgres

import (
	"context"
	"io"
	"log/slog"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"github.com/veryshyjelly/bifrost/migrations"
)

// Migrate explicitly applies embedded, forward-only PostgreSQL migrations.
func (s *Store) Migrate(ctx context.Context) error {
	database := stdlib.OpenDBFromPool(s.pool)
	defer database.Close()
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, database, migrations.Files,
		goose.WithSessionLocker(locker), goose.WithSlog(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		return err
	}
	defer provider.Close()
	_, err = provider.Up(ctx)
	return err
}
