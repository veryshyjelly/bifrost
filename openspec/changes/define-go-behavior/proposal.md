# Define behavior for the Go port

## Why

The Go port needs an explicit behavioral contract so a language and storage change does not lose delivery safety or owner workflows. Current behavior is now recorded separately from proposed improvements.

## What Changes

- Document the TypeScript baseline and review every capability with the owner.
- Require the OpenWA Go SDK and go-telegram/bot at their latest stable releases and review applicable new native features.
- **BREAKING**: replace runtime SQLite storage with PostgreSQL in the Go target, with an explicit state-preserving import path.
- Use Docker Compose for the Go bridge, OpenWA, Redis, local Telegram API and one PostgreSQL instance with separate app databases/users, as explicitly approved by the owner.
- Preserve all current route types and intentional deletion of the former management topic when moving to a dedicated route, as approved by the owner. Manual /history and /unmirror remain the recovery workflows.
- Preserve all other current behavior, explicitly approved by the owner, including linked-name formatting and intentional deletion rules.
- Support clearing outgoing captions when the latest released provider API permits it; otherwise retain the caption and explain the limitation.

## Capabilities

### New Capabilities

- `go-runtime-integration`: required provider libraries, release policy, PostgreSQL persistence, Compose setup and migration invariants for the future Go runtime.

### Modified Capabilities

- `message-relay`: allow clearing an outgoing caption when the latest released provider API supports it, preserving refusal, retry and uncertain-send handling.

## Impact

Implementation has begun with the PostgreSQL delivery foundation and diagnostic runtime. The full port affects provider clients, persistence, setup, transfer tooling and deployment. Existing HTTP endpoints and operational workflows remain the baseline until an explicit delta changes them. The current SQLite runtime and Compose setup remain operational during this documentation phase.
