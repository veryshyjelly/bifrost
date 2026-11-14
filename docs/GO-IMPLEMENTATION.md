# Go implementation design

The PostgreSQL delivery foundation and diagnostic runtime are now implemented; this is not a completed bridge. The approved contract is in OpenSpec. All existing behavior remains required, including linked names, intentional topic deletion and reviewed uncertain sends. Caption clearing is conditional on released provider support.

The [metadata snapshot/text stages](METADATA.md), [picture caching/uploads](METADATA-PICTURES.md) and [sync commands, permission advice and sealed reports](METADATA-ADVICE.md) now preserve source metadata, freeze destination-scoped updates/notices and retain independent receipts. [Durable run orchestration](METADATA-RUNS.md) now adds atomic admission, target discovery/fan-out, source serialization/coalescing, partial outcomes and persistent timezone cron. Explicit `run` composes these capabilities after native identity and callback verification; diagnostic `serve` remains passive.

## Shape and dependency direction

Build one modular application and one PostgreSQL database. Use `cmd/bifrost` as the composition root. Keep workflow logic in `internal/bridge`, durable execution in `internal/delivery`, PostgreSQL access in `internal/postgres`, HTTP routing in `internal/httpserver`, authenticated admission in `internal/ingress` and provider calls behind `internal/providers/openwa` and `internal/providers/telegram`.

Workflow packages define small interfaces at their point of use. Adapters implement those interfaces; they must not call back into workflow packages to route new work. Pass concrete dependencies through constructors. Prefer explicit Go types and functions, wrapped errors with errors.Is/As, context cancellation, bounded goroutines and structured log/slog output. Use standard net/http and ServeMux before adding a web framework. Avoid a dependency injection framework, generic service/repository base classes, global database/provider clients or a second queue service.

Create capability packages such as history, mentions and statuses when implementing those workflows; do not collapse them into an ever-growing Bridge object. Keep formatting and identity resolution independently testable. Introduce interfaces alongside the workflow and tests that consume them; avoid placeholder API designs.

## Tool choices

- Required provider modules: `github.com/veryshyjelly/OpenWA/sdk/go` and `github.com/go-telegram/bot`.
- PostgreSQL: pgx/v5 with pgxpool. Use explicit SQL and sqlc-generated pgx/v5 methods for persistence; keep SQL transactions visible rather than hiding them in an ORM.
- Migrations: Goose SQL migrations in `migrations/`, embedded into the eventual migration command. Migration execution is an explicit operation; starting the bridge must not accidentally run a destructive import.
- Formatting and static checks: gofmt and go vet. Add govulncheck with a pinned current version when runtime dependencies exist. Add other analyzers only for actionable coverage, not to accumulate overlapping linters.
- Tests: standard testing, httptest, temporary directories, fuzzing for hostile parsers and Go race detection. Persistence tests must run against real isolated PostgreSQL, not SQLite or SQL mocks. Testcontainers may be added if it improves lifecycle management; Compose is the setup contract.

[pgx transaction documentation](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool), [sqlc configuration](https://docs.sqlc.dev/en/latest/reference/config.html) and [Go module layout guidance](https://go.dev/doc/modules/layout) are the starting references. pgx transactions require explicit commit or rollback; cancellation alone does not close a transaction.

Versions resolved through `go list -m -json <module>@latest` on 2026-11-10:

| Module/tool | Latest resolved version |
| --- | --- |
| Go | 1.27.4 (also installed; [release history](https://go.dev/doc/devel/release)) |
| OpenWA SDK (maintained fork) | v0.11.1 |
| go-telegram/bot | v1.28.0 |
| pgx/v5 | v5.12.0 |
| Goose/v3 | v3.29.0 |
| sqlc | v1.31.1 |
| gopls | v0.24.0 (installed and verified) |

pgx/v5 and Goose are now runtime dependencies pinned in go.mod/go.sum, and sqlc is pinned as a Go tool dependency. Resolve latest again when adding the provider adapters. Do not retain unused imports solely to keep dependency declarations. PostgreSQL development uses 18.6, the current stable minor listed in the inspected official version policy; do not use a release candidate or floating latest image for database state.

## Delivery and PostgreSQL invariants

Persist accepted webhook events transactionally before responding. Use unique stable dedup keys and store owner/bot/account scope explicitly. Multiple copies and Telegram message IDs must retain bot scope. Preserve full-width IDs as text or suitable 64-bit values; do not round-trip them through floating point.

Claim jobs atomically and preserve stream order. A later job must not overtake an earlier pending, running, failed or uncertain job in its stream. If using FOR UPDATE SKIP LOCKED, add a stream-head guard: SKIP LOCKED alone does not preserve ordering. Use bounded worker concurrency and explicit claim ownership/leases. Recover only abandoned work; never reset another running process's claims merely by opening a connection.

Record an effect as sending before its external mutation. Commit its receipt after confirmed success. Provider calls happen outside database transactions; there is no transaction that atomically commits PostgreSQL and Telegram/WhatsApp. If the process stops in that gap, preserve uncertainty rather than inventing exactly-once guarantees. Completed effects are immutable. Retries reuse their prepared payload and original destination/key; only definitively undelivered parts may be sent again.

Distinguish known rate refusal and pre-connection failure from ambiguous mutations. Do not enable provider-wide automatic retries that resend timed-out mutations. HTTP acknowledgement, provider clients and workers all use the same delivery state model. History imports, live holds, notification cleanup and metadata priority must retain the baseline semantics.

The identity directory and mention resolver now connect to text-plan preparation through a narrow optional interface. Provider-verified aliases and contact binding changes serialize in PostgreSQL; native mention text and IDs freeze with the send plan. See [identity and mention boundaries](IDENTITY-MENTIONS.md) for source normalization and remaining caption/edit/history integration. The [mention picker and control replies](MENTION-PICKER.md) now include native copying, scoped callbacks, frozen refusal notices and expiry metadata. The [cleanup handlers](TELEGRAM-CLEANUP.md) schedule durable notice expiry and snapshot-scoped clearing with a separate idempotent deletion journal.

## Compose and cutover

The [private group picker and topic controls](GROUP-PICKER.md) now persist scoped selection requests, consume them with link commits and restore original conversation headers. Reply packets support frozen per-packet destinations and native reply keyboards. A narrow SDK serializer preserves the explicit private-group constraint; provider calls still use the required SDK. Explicit `run` composes these capabilities after native identity and callback verification; diagnostic `serve` remains passive.

[Manual mirror transitions](MIRROR-TRANSITIONS.md) now commit route changes and confirmation plans together. Topic creation retains normal uncertain-send review and persistent move/import guards; known refusals leave the original mirror active. Explicit `run` composes these capabilities after native identity and callback verification; diagnostic `serve` remains passive.

[Start help and command menus](STARTUP-CONTROLS.md) now include atomic member activation/direct-route admission and migration-gated menu jobs. Settings retain their original owner scope and completed steps across retry. Explicit `run` composes these capabilities after native identity and callback verification; diagnostic `serve` remains passive.

The [owner preference and read commands](OWNER-COMMANDS.md) now admit management work atomically with command plans. Preference execution commits its mode and frozen confirmation together; notice retry does not overwrite later owner decisions. Read receipts reuse their existing independent workflow.

The independent [former-inbox retirement handler](INBOX-RETIREMENT.md) reserves management topics before native deletion, refuses shared topics and waits for unfinished relays. Route and relay-plan admission serialize with these reservations; future importer and mirror-transition writes must preserve this invariant. General and fresh restorations remain protected.

The Compose project `bifrost-go` now owns the diagnostic bridge, one PostgreSQL instance with separate Bifrost/OpenWA databases and non-superuser roles, Redis, OpenWA and the official local Telegram API. A fresh instance is initialized by deploy/postgres/001-databases.sh; app roles cannot connect to each other’s databases. OpenWA builds a reviewed commit of the owner-maintained veryshyjelly/OpenWA fork by default, with an optional sibling-source override. All state uses new project volumes. OpenWA message storage uses PostgreSQL; its upstream boot/auth SQLite remains in openwa-data. No existing OpenWA installation or external network is required. Explicit `run` composes these capabilities after native identity and callback verification; diagnostic `serve` remains passive. See [text-relay boundaries](TEXT-RELAY.md). See [worker boundaries](WORKER.md). See [ingress setup](INGRESS.md).

Do not reuse the old SQLite data volume as a PostgreSQL volume. Do not start two bridge runtimes against the live identity. Implement import as an offline, explicit, validated transfer with a snapshot of the source. Preserve encryption keys, permanent identity, aliases, routes, bots, jobs, effects, mappings, history runs/receipts, prepared snapshots, mutes and cleanup state. Compare counts and identity/receipt keys before enabling outbound processing. Preserve uncertain states and immutable completed effects. Keep the original SQLite snapshot and a tested rollback path; migration does not mean resending old jobs.

## Outgoing WhatsApp sticker budget

The owner approved a 1,000,000-byte budget for the complete static or animated WebP, including metadata, based on the [live experiment](STICKER-LIMITS.md). The measured usable boundary was 1,048,558 bytes; 1,048,560 bytes was rejected during WhatsApp upload. Preserve an already valid animation within budget instead of re-encoding solely to meet the old 500,000-byte target. Validate container structure, decoded frames, dimensions and timing before freezing the outgoing payload; decoding or a send acknowledgement alone does not establish a renderable sticker. Telegram constraints remain separate. The Go codec workflow is still pending; this changes the target policy, not the live existing runtime.

## Implementation order and acceptance

1. Define typed configuration, safe errors, structured logs and PostgreSQL schema/migrations. Test key uniqueness, transaction rollback and permanent identity conflicts.
2. Implement queue claiming, effect ledger and restart recovery. Test duplicate admission, concurrent stream claiming, partial completion and crash ambiguity against PostgreSQL.
3. Implement authenticated HTTP ingress and required SDK adapters. Prove per-operation idempotency injection, error classification and webhook durability; verify caption clearing against the released engine, not just a Go string field.
4. Port routing, owner controls and messages, then formatting/mentions, media/stickers, history, Status feeds, reactions/revocations and metadata scheduling. Translate OpenSpec scenarios and historical fixtures into Go acceptance tests.
5. Implement setup/transfer/review commands, Compose services and SQLite import; validate a complete isolated migration/restart before any live cutover.

Each slice must pass focused tests and go test -race ./..., go vet, repository audit and OpenSpec validation. Codec and provider integration evidence must be reported separately from unit tests. The 263 historical passing tests remain reference evidence, not tests of the new Go runtime.
