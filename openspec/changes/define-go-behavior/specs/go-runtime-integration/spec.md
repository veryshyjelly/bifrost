# Go runtime integration

## ADDED Requirements

### Requirement: Required provider libraries

The Go bridge SHALL use github.com/rmyndharis/OpenWA/sdk/go for WhatsApp and github.com/go-telegram/bot for Telegram, using applicable native features instead of duplicating supported provider functionality.

#### Scenario: Provider integration

- **WHEN** a supported provider operation is implemented in Go
- **THEN** it uses the selected library while preserving the agreed behavioral contract

### Requirement: Latest stable releases

Implementation SHALL resolve the latest stable releases of the required libraries, gateway and supported API server at implementation start, pin exact resolved versions and recheck releases before completing the port. The nested OpenWA SDK module SHALL be resolved independently of the gateway release.

#### Scenario: New upstream release

- **WHEN** a newer stable dependency release appears before port completion
- **THEN** the port updates and validates compatibility, recording the exact tested versions

### Requirement: Feature coverage review

Every agreed provider-dependent capability SHALL be checked against current released native features; missing coverage SHALL be recorded and reviewed before a workaround or behavior reduction is selected.

#### Scenario: Required API missing

- **WHEN** the latest released SDK lacks a required media or managed-bot capability
- **THEN** the gap and its behavior impact are documented without silently dropping the capability or using unreleased main

### Requirement: PostgreSQL persistence

The Go runtime SHALL use PostgreSQL for all durable bridge state instead of SQLite, preserving transactional deduplication, ordering, effect receipts, encrypted tokens, identity, preferences and import checkpoints.

#### Scenario: Runtime restarts

- **WHEN** the Compose bridge container is recreated against its existing PostgreSQL database
- **THEN** durable state remains available and completed delivery effects are not replayed

### Requirement: State migration

The port SHALL provide an explicit validated SQLite-to-PostgreSQL migration path preserving BIFROST_ACCOUNT_ID, encryption keys, stable delivery keys, receipts, aliases, mappings, control state and uncertain jobs. Migration SHALL not silently reset state or resume source writes before cutover checks succeed.

#### Scenario: Migrate uncertain send

- **WHEN** the SQLite source contains an uncertain outgoing send and completed effects
- **THEN** PostgreSQL retains the uncertainty and completed receipts; the send still requires review

### Requirement: Docker Compose setup

The Go setup SHALL use Docker Compose for the bridge, PostgreSQL and local Telegram API, with persistent state and media storage, database readiness checks and private configurable credentials. Existing external OpenWA connectivity SHALL remain supported.

#### Scenario: Database starts slowly

- **WHEN** Compose starts the bridge before PostgreSQL is ready
- **THEN** the bridge waits or retries readiness without discarding state or accepting undurable delivery

### Requirement: Delivery safety with SDKs

Provider integration SHALL preserve stable per-send idempotency, durable admission before webhook acknowledgement and explicit review of ambiguous mutation outcomes; SDK automatic retries SHALL not bypass these rules.

#### Scenario: Ambiguous provider send

- **WHEN** a library call times out after the request may have reached the provider
- **THEN** the send remains uncertain and is not automatically replayed by SDK retry middleware
