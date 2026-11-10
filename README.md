# Bifrost

A personal WhatsApp–Telegram bridge being ported to Go. The [approved behavior contract](openspec/README.md) preserves the existing routing, formatting, deletion and delivery semantics, with conditional caption clearing when the latest released provider API supports it.

This branch is prepared for implementation. It contains Go package boundaries and checks, but no runnable Go bridge yet. The TypeScript runtime and tests are recoverable at commit `checkpoint-typescript-baseline`; [historical deployment and recovery documentation](docs/legacy/README.md) remains available.

## Development

Use Go 1.27.3 and Python 3 only when working with the retained migration/codec helpers.

```sh
make check
make spec-check
```

`make check` verifies formatting, vet, race-enabled package compilation/tests, repository hygiene and Compose configuration. Current Go packages are scaffolding with no behavior tests; the historical suite had 263 passing and 11 skipped tests. Port the OpenSpec acceptance scenarios as implementation proceeds.

## PostgreSQL development setup

```sh
cp .go.env.example .go.env
# Choose a private POSTGRES_PASSWORD in .go.env.
docker compose -p bifrost-go --env-file .go.env up -d postgres
```

The preparation Compose file runs PostgreSQL only, using a separate project and volume. It does not deploy a replacement bridge or migrate the existing SQLite database. Add the Go bridge and local Telegram API services after their implementations and release compatibility checks exist. Keep existing `.env`, `bridge.env`, `telegram-api.env`, data, media, backups and Docker volumes intact.

Read the [Go implementation design](docs/GO-IMPLEMENTATION.md) and [MCP verification record](docs/MCP-SETUP.md) before adding runtime code. The required provider libraries are OpenWA's Go SDK and go-telegram/bot; runtime persistence will use PostgreSQL with pgx, explicit SQL/sqlc and Goose migrations.
