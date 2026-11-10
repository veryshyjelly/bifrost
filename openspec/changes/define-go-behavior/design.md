# Behavior documentation design

## Current state

Baseline source: `checkpoint-go-contract-baseline`, branch `port/golang`. Bun/TypeScript uses SQLite jobs, effect receipts, mappings, encrypted bot tokens and JSON state. The existing Compose file runs Bifrost and a local Telegram Bot API server, shares media volumes and connects to an external OpenWA network.

## Documentation decisions

- Main specs describe current observable behavior. Change specs describe approved target differences.
- The required Go integration is specified now because the owner explicitly selected both libraries, PostgreSQL and Docker Compose.
- The owner approved preserving all current behavior after explicitly confirming linked-name formatting and deletion rules. Add only conditional caption clearing to message behavior.
- Acceptance scenarios define behavior; source/fixture references establish evidence. Live provider compatibility, codec fixtures and Docker transfer verification remain separate evidence levels.

## Target integration constraints

Use the two required provider clients and their current typed/native functionality. Resolve each Go module independently at implementation time; the OpenWA gateway release tag does not establish the nested SDK module version. Pin exact versions and record the tested gateway/Telegram API compatibility. Do not silently use an unreleased branch to satisfy a missing feature.

PostgreSQL replaces SQLite for all durable runtime state, not just new tables. Atomic enqueue/deduplication, job claiming, immutable completed effects and uncertainty recovery must survive the change. The first implementation slice selects PostgreSQL 18.6, pgx/v5, sqlc-generated queries and embedded Goose migrations for identity, jobs and effects. Remaining schemas, import tooling and rollback/cutover procedures belong to subsequent slices. Preserve opaque encrypted values and stable keys during migration; do not reset or replay delivery state.

The owner subsequently required the complete setup in this Compose project, including OpenWA and Redis, and selected one PostgreSQL instance with separate restricted Bifrost/OpenWA databases/users. Fresh setup builds the stable OpenWA release without requiring its sibling checkout; an explicit local source override is supported. Shared database bootstrap applies only to fresh volumes. Go implementation has begun with delivery persistence and diagnostics; no live cutover or provider relaying is enabled.

## Important compatibility checks for the port

- SDK retries must not replay ambiguous mutations; preserve the existing reviewed-send contract.
- Telegram SDK webhook processing must acknowledge only after durable admission and preserve per-conversation ordering.
- Verify managed-bot creation/token APIs, native date/time entities, custom-emoji reactions, forum permissions and private bot-scoped mappings against the selected release and local API server.
- Verify per-send OpenWA idempotency headers, caption edit wire format, deep history, bounded source-media download and passive Status reads against the independently resolved SDK/gateway versions.
- The SDK's mutable main documentation is preliminary evidence, not proof of release coverage. Missing required coverage must be documented for review before any workaround is chosen.

## Review gate

All five review groups are approved by the owner, with conditional caption clearing as the only message behavior change. The behavior contract is complete. Keep this change open until the Go requirements are implemented; proceed next to a separate implementation design covering exact SDK coverage and PostgreSQL migration. Do not confuse complete planning artifacts with an implemented Go runtime.
