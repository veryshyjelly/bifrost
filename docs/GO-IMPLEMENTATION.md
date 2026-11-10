# Go implementation design

This is the implementation foundation, not a completed bridge. The approved contract is in OpenSpec. All existing behavior remains required, including linked names, intentional topic deletion and reviewed uncertain sends. Caption clearing is conditional on released provider support.

## Shape and dependency direction

Build one modular application and one PostgreSQL database. Use `cmd/bifrost` as the composition root. Keep workflow logic in `internal/bridge`, durable execution in `internal/delivery`, PostgreSQL access in `internal/postgres`, HTTP ingress in `internal/httpserver` and provider calls behind `internal/providers/openwa` and `internal/providers/telegram`.

Workflow packages define small interfaces at their point of use. Adapters implement those interfaces; they must not call back into workflow packages to route new work. Pass concrete dependencies through constructors. Prefer explicit Go types and functions, wrapped errors with errors.Is/As, context cancellation, bounded goroutines and structured log/slog output. Use standard net/http and ServeMux before adding a web framework. Avoid a dependency injection framework, generic service/repository base classes, global database/provider clients or a second queue service.

Create capability packages such as history, mentions and statuses when implementing those workflows; do not collapse them into an ever-growing Bridge object. Keep formatting and identity resolution independently testable. No business interfaces or wire schemas are invented by the preparation scaffold.

## Tool choices

- Required provider modules: `github.com/rmyndharis/OpenWA/sdk/go` and `github.com/go-telegram/bot`.
- PostgreSQL: pgx/v5 with pgxpool. Use explicit SQL and sqlc-generated pgx/v5 methods for persistence; keep SQL transactions visible rather than hiding them in an ORM.
- Migrations: Goose SQL migrations in `migrations/`, embedded into the eventual migration command. Migration execution is an explicit operation; starting the bridge must not accidentally run a destructive import.
- Formatting and static checks: gofmt and go vet. Add govulncheck with a pinned current version when runtime dependencies exist. Add other analyzers only for actionable coverage, not to accumulate overlapping linters.
- Tests: standard testing, httptest, temporary directories, fuzzing for hostile parsers and Go race detection. Persistence tests must run against real isolated PostgreSQL, not SQLite or SQL mocks. Testcontainers may be added if it improves lifecycle management; Compose is the setup contract.

[pgx transaction documentation](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool), [sqlc configuration](https://docs.sqlc.dev/en/latest/reference/config.html) and [Go module layout guidance](https://go.dev/doc/modules/layout) are the starting references. pgx transactions require explicit commit or rollback; cancellation alone does not close a transaction.

Versions resolved through `go list -m -json <module>@latest` on 2026-11-10:

| Module/tool | Latest resolved version |
| --- | --- |
| Go | 1.27.3 (also installed) |
| OpenWA SDK | v0.10.0 |
| go-telegram/bot | v1.28.0 |
| pgx/v5 | v5.12.0 |
| Goose/v3 | v3.29.0 |
| sqlc | v1.31.1 |
| gopls | v0.24.0 (installed and verified) |

The empty Go module has no runtime dependencies yet. Resolve latest again and add exact requirements plus go.sum when the first adapters/storage implementation actually uses them; do not retain unused imports solely to keep dependency declarations. Tool versions will be pinned using Go tool dependencies when their configurations and inputs exist. PostgreSQL development uses 18.6, the current stable minor listed in the inspected official version policy; do not use a release candidate or floating latest image for database state.

## Delivery and PostgreSQL invariants

Persist accepted webhook events transactionally before responding. Use unique stable dedup keys and store owner/bot/account scope explicitly. Multiple copies and Telegram message IDs must retain bot scope. Preserve full-width IDs as text or suitable 64-bit values; do not round-trip them through floating point.

Claim jobs atomically and preserve stream order. A later job must not overtake an earlier pending, running, failed or uncertain job in its stream. If using FOR UPDATE SKIP LOCKED, add a stream-head guard: SKIP LOCKED alone does not preserve ordering. Use bounded worker concurrency and explicit claim ownership/leases. Recover only abandoned work; never reset another running process's claims merely by opening a connection.

Record an effect as sending before its external mutation. Commit its receipt after confirmed success. Provider calls happen outside database transactions; there is no transaction that atomically commits PostgreSQL and Telegram/WhatsApp. If the process stops in that gap, preserve uncertainty rather than inventing exactly-once guarantees. Completed effects are immutable. Retries reuse their prepared payload and original destination/key; only definitively undelivered parts may be sent again.

Distinguish known rate refusal and pre-connection failure from ambiguous mutations. Do not enable provider-wide automatic retries that resend timed-out mutations. HTTP acknowledgement, provider clients and workers all use the same delivery state model. History imports, live holds, notification cleanup and metadata priority must retain the baseline semantics.

## Compose and cutover

The current preparation Compose file starts only isolated PostgreSQL under project `bifrost-go`. The bridge binary and its Dockerfile do not exist yet. Add the bridge and current official local Telegram API services after adapter implementation, using readiness checks, persistent named volumes, restricted host bindings and exact verified image versions. Retain external OpenWA connectivity.

Do not reuse the old SQLite data volume as a PostgreSQL volume. Do not start two bridge runtimes against the live identity. Implement import as an offline, explicit, validated transfer with a snapshot of the source. Preserve encryption keys, permanent identity, aliases, routes, bots, jobs, effects, mappings, history runs/receipts, prepared snapshots, mutes and cleanup state. Compare counts and identity/receipt keys before enabling outbound processing. Preserve uncertain states and immutable completed effects. Keep the original SQLite snapshot and a tested rollback path; migration does not mean resending old jobs.

## Implementation order and acceptance

1. Define typed configuration, safe errors, structured logs and PostgreSQL schema/migrations. Test key uniqueness, transaction rollback and permanent identity conflicts.
2. Implement queue claiming, effect ledger and restart recovery. Test duplicate admission, concurrent stream claiming, partial completion and crash ambiguity against PostgreSQL.
3. Implement authenticated HTTP ingress and required SDK adapters. Prove per-operation idempotency injection, error classification and webhook durability; verify caption clearing against the released engine, not just a Go string field.
4. Port routing, owner controls and messages, then formatting/mentions, media/stickers, history, Status feeds, reactions/revocations and metadata scheduling. Translate OpenSpec scenarios and historical fixtures into Go acceptance tests.
5. Implement setup/transfer/review commands, Compose services and SQLite import; validate a complete isolated migration/restart before any live cutover.

Each slice must pass focused tests and go test -race ./..., go vet, repository audit and OpenSpec validation. Codec and provider integration evidence must be reported separately from unit tests. The 263 historical passing tests remain reference evidence, not tests of the new Go runtime.
