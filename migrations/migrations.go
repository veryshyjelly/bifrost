// Package migrations embeds the PostgreSQL schema for explicit migration commands.
package migrations

import "embed"

// Files contains versioned Goose migrations.
//
//go:embed *.sql
var Files embed.FS
