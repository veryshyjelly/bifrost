# Repository Guidelines

Bifrost is being ported to Go on `port/golang`. The approved behavior contract is in `openspec/`. Preserve every current behavior except the explicit conditional caption-clearing change. Linked-name formatting, source deletion choices and former-topic deletion are intentional.

## Structure and design

Use one Go module, `cmd/bifrost` for composition, capability-focused `internal/` packages, narrow consumer-defined interfaces, explicit dependency injection and provider adapters. See `docs/GO-IMPLEMENTATION.md`. PostgreSQL delivery persistence and a diagnostic runtime are implemented; SDK clients and explicit provider diagnostics are implemented; automatic provider workflows and legacy import are not implemented yet.

Use PostgreSQL with pgx/v5, explicit SQL/sqlc and Goose migrations. Use the OpenWA Go SDK and go-telegram/bot, strictly resolving latest stable releases and pinning exact versions when implementing their first real use. Verify current native features and API coverage. The gateway comes from the owner-maintained https://github.com/veryshyjelly/OpenWA fork at an explicitly reviewed commit; preserve its media/mention patches when updating the upstream base. Use the maintained Go SDK module github.com/veryshyjelly/OpenWA/sdk/go; its releases are independent of the gateway and other language SDKs. Do not silently replace libraries, drop features or float the gateway build on main.

Use standard context, errors, net/http, log/slog and testing where they suffice. Run gofmt; prefer idiomatic Go naming and small packages over broad frameworks or generic repositories.

## Commands and tests

- `make check`: formatting, go vet, race-enabled Go tests, repository audit and Compose validation.
- `make spec-check`: strict OpenSpec validation (requires installed OpenSpec).
- `go test ./internal/<package>`: focused tests once implementations exist.
- `make configure`, then fill Telegram API credentials and run `make up`: complete isolated Compose setup with one PostgreSQL instance (separate app databases/users), Redis, OpenWA, Telegram API and Bifrost.

Package checks cover the implemented persistence/diagnostic slice, not full behavior parity. Port observable OpenSpec scenarios and historical fixtures into Go. Use real isolated PostgreSQL for persistence/concurrency tests. Cover duplicate events, retries, partial/uncertain delivery, owner checks, crash recovery and migration. No numeric coverage target is imposed.

## History and operational safety

The full TypeScript baseline, tests and original deployment exist at commit `checkpoint-typescript-baseline`. Historical operational docs and examples are in `docs/legacy/`; they require that checkout. Source and fixture links in OpenSpec point to the checkpoint. Use `git show checkpoint-typescript-baseline:<path>` before the checkpoint is pushed.

Keep private environments, SQLite databases, media, backups and Docker volumes intact. Never reset permanent BIFROST_ACCOUNT_ID, encryption keys, stable delivery keys or receipts. Preserve uncertain sends for explicit review. Do not start two runtimes against the live identity or perform a live cutover as part of preparation. Follow historical MOVING/RECOVERY instructions before identity import.

Keep commits focused with plain imperative subjects. Describe behavior, validation and remaining integration limits in PRs. Do not introduce new behavior without an OpenSpec change.
