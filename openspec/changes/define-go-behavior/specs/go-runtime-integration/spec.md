# Go runtime integration

## ADDED Requirements

### Requirement: Required provider libraries

The Go bridge SHALL use github.com/veryshyjelly/OpenWA/sdk/go for WhatsApp and github.com/go-telegram/bot for Telegram, using applicable native features instead of duplicating supported provider functionality.

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

The Go setup SHALL use Docker Compose for Bifrost, OpenWA, the local Telegram API, Redis and one PostgreSQL instance, with persistent state/media, readiness checks and private credentials. It SHALL require no existing OpenWA installation or external network.

#### Scenario: Database starts slowly

- **WHEN** Compose starts the bridge before PostgreSQL is ready
- **THEN** the bridge waits or retries readiness without discarding state or accepting undurable delivery

#### Scenario: Fresh complete stack

- **WHEN** an owner generates private configuration, fills Telegram API credentials and runs the documented startup command
- **THEN** Compose builds/starts Bifrost, OpenWA, the local Telegram API, Redis and PostgreSQL without an external OpenWA checkout
- **AND** Bifrost and OpenWA use separate databases and restricted users on one PostgreSQL instance

#### Scenario: Existing state during setup

- **WHEN** configuration is generated again or a PostgreSQL volume already contains data
- **THEN** configuration keys are not overwritten and database bootstrap does not recreate existing roles, databases or delivery state


### Requirement: Application database isolation

Bifrost and OpenWA SHALL use separate databases and non-superuser roles on the shared PostgreSQL instance. App roles SHALL be denied access to each other’s databases. Database bootstrap SHALL run only on an empty data volume.

#### Scenario: Cross-application access

- **WHEN** Bifrost credentials are used to connect to the OpenWA database, or OpenWA credentials are used for the Bifrost database
- **THEN** PostgreSQL refuses the connection without granting elevated privileges

### Requirement: OpenWA source and persistent state

Fresh setup SHALL build a reviewed commit of the owner-maintained veryshyjelly/OpenWA fork, preserving its media/mention patches over the upstream release base without requiring a sibling checkout. An explicit source override SHALL support local development. OpenWA’s required boot/auth and browser state SHALL persist on its own data volume alongside PostgreSQL message storage.

#### Scenario: Local source override

- **WHEN** the owner explicitly sets OPENWA_SOURCE to the sibling OpenWa checkout
- **THEN** Compose builds that source without modifying the sibling installation or reusing its runtime volumes


### Requirement: Delivery safety with SDKs

Provider integration SHALL preserve stable per-send idempotency, durable admission before webhook acknowledgement and explicit review of ambiguous mutation outcomes; SDK automatic retries SHALL not bypass these rules.

#### Scenario: Ambiguous provider send

- **WHEN** a library call times out after the request may have reached the provider
- **THEN** the send remains uncertain and is not automatically replayed by SDK retry middleware

### Requirement: Measured outgoing WhatsApp sticker budget

The Go bridge SHALL use a maximum complete-file encoding budget of 1,000,000 bytes for both static and animated outgoing WhatsApp WebP stickers, including metadata, while preserving animation and quality within the budget. It SHALL validate WebP container structure, decoded frames, dimensions and timing before freezing the outgoing payload. Telegram sticker constraints SHALL remain separate. This approved target change is based on docs/STICKER-LIMITS.md, where usable files reached 1,048,558 bytes and upload rejected 1,048,560 bytes.

#### Scenario: Valid animation exceeds the old target

- **WHEN** an outgoing WhatsApp animation is structurally valid, meets dimension/timing requirements and is 800,000 bytes including metadata
- **THEN** it remains eligible for animated delivery without re-encoding solely to meet the former 500,000-byte target

#### Scenario: Metadata pushes an encoding over budget

- **WHEN** the complete outgoing WebP exceeds 1,000,000 bytes after metadata is added
- **THEN** the encoder adjusts it to fit before freezing or sending the payload, following the existing fallback behavior if it cannot produce a valid sticker

#### Scenario: Decodable fixture has an unsuitable container

- **WHEN** a file decodes locally but fails structural validation, such as trailing bytes outside the declared RIFF container
- **THEN** local decoding or an API acknowledgement does not establish sticker validity and the bridge repairs or re-encodes the file before sending
