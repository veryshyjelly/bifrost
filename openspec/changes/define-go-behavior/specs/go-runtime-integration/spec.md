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

The Go bridge SHALL use a 1,000,000-byte complete-file budget, including metadata, for static and animated outgoing WhatsApp WebP stickers. It SHALL preserve animation and quality within budget and validate container structure, decoded frames, dimensions and timing before freezing the payload. Telegram constraints SHALL remain separate. Evidence: docs/STICKER-LIMITS.md.

#### Scenario: Valid animation exceeds the old target

- **WHEN** an outgoing WhatsApp animation is structurally valid, meets dimension/timing requirements and is 800,000 bytes including metadata
- **THEN** it remains eligible for animated delivery without re-encoding solely to meet the former 500,000-byte target

#### Scenario: Metadata pushes an encoding over budget

- **WHEN** the complete outgoing WebP exceeds 1,000,000 bytes after metadata is added
- **THEN** the encoder adjusts it to fit before freezing or sending the payload, following the existing fallback behavior if it cannot produce a valid sticker

#### Scenario: Decodable fixture has an unsuitable container

- **WHEN** a file decodes locally but fails structural validation, such as trailing bytes outside the declared RIFF container
- **THEN** local decoding or an API acknowledgement does not establish sticker validity and the bridge repairs or re-encodes the file before sending

### Requirement: Explicit handling of definitely undelivered effects

The Go implementation SHALL permit a definitely undelivered effect to become terminal after an explicitly handled refusal or an authorized muted/ignored route decision, without representing it as successful. Its frozen request and existing completed receipts SHALL remain retained. Sending, uncertain and completed effects SHALL NOT be discarded.

#### Scenario: Definite topic refusal is handled

- **WHEN** topic creation is definitively refused and a workflow chooses to send the existing owner guidance instead
- **THEN** it explicitly closes the undelivered branch, retains its frozen request and can finish after the notice succeeds

#### Scenario: Uncertain operation cannot be discarded

- **WHEN** an effect is sending or its outcome is uncertain
- **THEN** handled-refusal logic cannot discard it or bypass explicit review

#### Scenario: Retry after a handled refusal

- **WHEN** a later part retries after a definitively refused branch was explicitly closed
- **THEN** that closed branch is not sent again

#### Scenario: Owner pauses a partial relay

- **WHEN** a route becomes muted or ignored after some parts completed and other parts are definitely unsent
- **THEN** only the unsent parts close without delivery, completed copies remain mapped, and no sending or uncertain operation is discarded

### Requirement: Canonical source snapshots and stream continuity

The Go implementation SHALL admit complete native WhatsApp messages with an immutable source snapshot and a shared conversation stream in one transaction. Verified aliases SHALL retain an established conversation key without rewriting routes, mappings or stable delivery keys. Independently established conflicting conversations SHALL require review before merging. Recovery of older raw jobs SHALL preserve their IDs and fence stream changes against claims.

#### Scenario: Phone identity learned after a LID conversation exists

- **WHEN** verified phone evidence arrives for a LID with an established conversation
- **THEN** later phone and LID events share that conversation queue while retaining the original route and receipts

#### Scenario: Older raw job meets an active canonical job

- **WHEN** recovery moves a raw job into a conversation with another active job
- **THEN** it releases its old claim and waits without sending, changing delivery keys or consuming a retry attempt

#### Scenario: Identity evidence conflicts during admission

- **WHEN** source preparation cannot safely reconcile verified identities or independently established conversations
- **THEN** the original event remains durable for review and partial alias/profile changes do not commit

### Requirement: Frozen owner control operations

The Go implementation SHALL persist complete control replies and scoped navigation actions before sending. Known retries SHALL reuse their original text, entities, buttons and effect keys. Callback acknowledgements SHALL retain their own effect receipts; a definite expired-query refusal MAY close without claiming success. Cleanup deadlines SHALL persist without making conversation messages eligible for automatic expiry.

#### Scenario: Picker send is rate limited

- **WHEN** a prepared picker reply receives a known rate refusal and the roster or bot metadata changes
- **THEN** its retry retains the original page, copied tags and callback actions

#### Scenario: Callback acknowledgement already expired

- **WHEN** Telegram definitively refuses the acknowledgement of an otherwise valid picker action with HTTP 400
- **THEN** only that acknowledgement closes without success and the prepared page can complete without repeating completed effects

#### Scenario: Refusal notice retries after validation data changes

- **WHEN** an outgoing mention was refused and its owner notice needs retrying
- **THEN** the notice retains its original delivery key and the refused message is not reconsidered as a WhatsApp send

### Requirement: Durable scoped Telegram cleanup

The Go implementation SHALL persist expiry jobs with their recorded deadlines and freeze manual-clear membership before deleting. Snapshot membership SHALL exclude later committed observations, including rows with earlier sequence values. Idempotent Telegram deletions SHALL use a separate fenced cleanup journal so response loss can retry the same IDs without weakening uncertain-send review. Cleanup SHALL preserve mappings and explicit deletion markers.

#### Scenario: Clear snapshot meets a late transaction

- **WHEN** a transaction commits an observation after the clear snapshot, even with a sequence below its cutoff
- **THEN** that observation is excluded from the clear run

#### Scenario: Cleanup loses a deletion response

- **WHEN** a frozen Telegram deletion has an ambiguous response or its local outcome cannot commit
- **THEN** retry can repeat only the same idempotent deletion targets, while recorded outcomes and ordinary message sends keep their own recovery rules

#### Scenario: Notice cleanup meets an unfinished relay receipt

- **WHEN** an own-bot observation may be a conversation copy whose relay receipt or mapping is unresolved
- **THEN** notice cleanup waits for resolution or excludes the confirmed relay copy rather than deleting it as a notice

### Requirement: Fenced former inbox retirement

The Go implementation SHALL retire eligible former management topics independently of delivery, preserving intentional deletion while excluding General and shared topics. It SHALL freeze deletion targets, serialize reservations with routing and relay-plan admission, wait for incomplete relays and retain deletion outcomes separately from uncertain message sends.

#### Scenario: Permission refusal during bot activation cleanup

- **WHEN** a bound bot is started and Telegram refuses deletion of its former management topic
- **THEN** private conversation delivery continues and the separate cleanup job retains its original topic for explicit retry

#### Scenario: A competing route has not committed

- **WHEN** a route transaction holding the shared admission lock assigns another conversation to a retirement candidate
- **THEN** retirement waits, observes that committed route and refuses deletion without partial reservations

#### Scenario: Topic deletion loses its response before a fresh restoration

- **WHEN** a former-topic deletion response is lost and the conversation later returns to a fresh management topic
- **THEN** retry deletes only the originally reserved topic, cannot rebind it as a live target and preserves the fresh inbox

#### Scenario: Incomplete relay still targets the old inbox

- **WHEN** a prepared relay has unfinished or uncertain work at a former management topic
- **THEN** retirement waits for that relay to resolve, retaining its frozen destination and receipts before deletion

### Requirement: Durable conversation owner decisions

The Go implementation SHALL admit preference and explicit read commands with durable command plans. Preference execution SHALL commit its mode and complete confirmation plan together. Notice recovery SHALL preserve the original reply without re-applying an older preference over later decisions. Read retries SHALL remain independent of message sends.

#### Scenario: Action admission fails before acknowledgement

- **WHEN** a source command cannot commit its prepared control plan
- **THEN** the original ingress remains durable while partial management admission, expiry scheduling and preference changes do not commit

#### Scenario: An old confirmation retries after a later decision

- **WHEN** a preference confirmation retries after the owner has issued a newer decision or changed routes
- **THEN** it reuses the original notice and receipts without re-applying the older mode or moving the confirmation

#### Scenario: Explicit read is refused

- **WHEN** the provider refuses an explicitly queued read receipt
- **THEN** its independent retry uses the resolved WhatsApp chat without repeating a message send or changing conversation preferences

### Requirement: Durable Start activation

The Go implementation SHALL preserve ordinary Start help, activation and independent inbox retirement. Member activation, any missing direct route, cleanup admission and the frozen help plan SHALL commit together. Retries SHALL retain original guidance without reconsidering later bindings. Dedicated deep links SHALL remain delegated to their workflows.

#### Scenario: Help plan cannot commit

- **WHEN** private member activation cannot persist its complete help plan
- **THEN** activation, direct-route creation and cleanup admission roll back while the original source event remains durable

#### Scenario: Binding changes during a help retry

- **WHEN** an unbound member's prepared help retries after that bot has been bound
- **THEN** the older reply retains its original guidance and does not create a new route or cleanup job from that retry

### Requirement: Migration-gated durable command menus

The Go implementation SHALL admit startup menus only for explicitly migrated bots and preserve legacy command lists and owner scope. It SHALL freeze settings before native calls and journal idempotent setting outcomes independently of message sends. Completed settings SHALL survive restart; configuration changes SHALL NOT retarget an existing installation.

#### Scenario: Registered bot has not migrated

- **WHEN** a registered or started bot lacks a validated local API migration marker
- **THEN** startup does not queue menu installation for it

#### Scenario: Menu setting response is lost

- **WHEN** a menu setting response or its local progress commit is lost
- **THEN** retry repeats only the original idempotent settings while preserving completed steps and ordinary message-send uncertainty rules

#### Scenario: Configuration changes before menu recovery

- **WHEN** a new consumer resumes a partially completed menu installation after configuration changes
- **THEN** it retains the original command list, management chat and owner scope and records installation only after all settings complete
