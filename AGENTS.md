# Repository Guidelines

Bifrost is being ported to Go on `port/golang`. The approved behavior contract is in `openspec/`. Preserve every current behavior except the explicit conditional caption-clearing change. Linked-name formatting, source deletion choices and former-topic deletion are intentional.

## Structure and design

Use one Go module, `cmd/bifrost` for composition, capability-focused `internal/` packages, narrow consumer-defined interfaces, explicit dependency injection and provider adapters. See `docs/GO-IMPLEMENTATION.md`. The current packages are scaffolding; no runnable Go bridge exists yet.

Use PostgreSQL with pgx/v5, explicit SQL/sqlc and Goose migrations. Use the OpenWA Go SDK and go-telegram/bot, strictly resolving latest stable releases and pinning exact versions when implementing their first real use. Verify current native features and API coverage. Do not silently replace libraries, drop features or use unreleased main.

Use standard context, errors, net/http, log/slog and testing where they suffice. Run gofmt; prefer idiomatic Go naming and small packages over broad frameworks or generic repositories.

## Commands and tests

- `make check`: formatting, go vet, race-enabled Go tests, repository audit and Compose validation.
- `make spec-check`: strict OpenSpec validation (requires installed OpenSpec).
- `go test ./internal/<package>`: focused tests once implementations exist.
- `docker compose -p bifrost-go --env-file .go.env up -d postgres`: isolated development PostgreSQL, after copying `.go.env.example` and selecting a private password.

Current package checks prove scaffold compilation, not behavior parity. Port observable OpenSpec scenarios and historical fixtures into Go. Use real isolated PostgreSQL for persistence/concurrency tests. Cover duplicate events, retries, partial/uncertain delivery, owner checks, crash recovery and migration. No numeric coverage target is imposed.

## History and operational safety

The full TypeScript baseline, tests and original deployment exist at commit `checkpoint-typescript-baseline`. Historical operational docs and examples are in `docs/legacy/`; they require that checkout. Source and fixture links in OpenSpec point to the checkpoint. Use `git show checkpoint-typescript-baseline:<path>` before the checkpoint is pushed.

Keep private environments, SQLite databases, media, backups and Docker volumes intact. Never reset permanent BIFROST_ACCOUNT_ID, encryption keys, stable delivery keys or receipts. Preserve uncertain sends for explicit review. Do not start two runtimes against the live identity or perform a live cutover as part of preparation. Follow historical MOVING/RECOVERY instructions before identity import.

Keep commits focused with plain imperative subjects. Describe behavior, validation and remaining integration limits in PRs. Do not introduce new behavior without an OpenSpec change.
