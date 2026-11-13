# Go runtime integration

## ADDED Requirements

### Requirement: Explicit topic reconciliation

The Go runtime SHALL provide authenticated inspection and reconciliation for interrupted initial inbox, mirror return, native delivery repair and Status topic creation. Inspection SHALL expose the permanent account, job/effect identity, frozen management target/name, request digest and current states without credentials. Generic skip SHALL retain unresolved topic ownership and route guards.

#### Scenario: Adopt a topic after a lost acknowledgement

- **WHEN** the operator supplies the inspected account/job/effect/request digest, expected states and exact created management topic with explicit confirmation
- **THEN** the bridge records the original topic effect receipt, preserves its request/key, resumes the same job and retains its guard until the normal route/feed commit, without creating another topic

#### Scenario: Confirm that no topic was created

- **WHEN** the operator explicitly confirms absence against the unchanged inspected request and states
- **THEN** only that unresolved topic effect is reset for the original job to retry; unrelated uncertain effects, completed receipts and permanent reservations remain protected

#### Scenario: Stale or conflicting review

- **WHEN** the account, request digest, expected state, route generation or topic reservation conflicts, a job is live, or another send is uncertain
- **THEN** reconciliation is rejected without changing effects, routes, job state or guards

#### Scenario: Repeated reconciliation request

- **WHEN** the same reconciliation operation identifier and exact input are submitted again
- **THEN** the recorded result is returned without reapplying the decision or resetting subsequent work

#### Scenario: Crash before effect preparation

- **WHEN** a supported topic guard has a frozen request but its effect was never prepared
- **THEN** inspection reports that state and an explicitly confirmed reconciliation resumes only that original request

#### Scenario: Existing confirmed receipt

- **WHEN** the topic effect already has a completed receipt
- **THEN** absence or replacement is refused and resuming that exact topic leaves the original request and receipt unchanged

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

#### Scenario: Inspect a frozen legacy export

- **WHEN** an operator validates a legacy v1 export before import
- **THEN** the Go reader checks component hashes, archive safety, SQLite integrity and committed WAL data, original identity/key and scoped relationships without modifying source resources or contacting providers
- **AND** exact integer IDs, encrypted credential strings, frozen bytes and sending/running states remain source evidence, with no fabricated receipts or replay eligibility

#### Scenario: Unsafe or unsupported legacy input

- **WHEN** the export has unsafe paths, links, duplicate entries or JSON keys, damaged checksums, unknown durable domains, conflicting identity evidence or invalid private/group scopes
- **THEN** validation refuses the input and removes only its owned temporary stage, leaving existing configurations, databases and volumes untouched

#### Scenario: Transactional import preparation

- **WHEN** a validated archive is prepared in an empty PostgreSQL destination
- **THEN** its original identity, exact source rows and encrypted configuration checkpoint commit atomically after all validation, or roll back together on failure
- **AND** concurrent writers and nonempty/conflicting destinations are refused; repeating the exact archive returns its original result without resetting later work

#### Scenario: Preparation is incomplete

- **WHEN** source evidence has been prepared but native journals and retained assets have not been activated
- **THEN** the account cannot pass runtime readiness or claim any job, and preparation does not fabricate effects or runnable imported work

#### Scenario: Imported Telegram setup evidence

- **WHEN** legacy bot credentials and a confirmed hosted-logout flag are projected
- **THEN** original member ciphertexts, account/bot scope and Start activation remain intact, and only the matching credential revision can skip hosted logout
- **AND** cached identity, registration, Start and the legacy flag do not establish local API migration eligibility; native local identity verification still precedes webhook/menu readiness

#### Scenario: Credential projection retry

- **WHEN** credential projection fails or its exact completed checkpoint is repeated after a subsequent credential change
- **THEN** failure rolls back credentials and source proof together, while completed replay returns its first result without restoring old tokens over later state

#### Scenario: Imported identity and copied mentions

- **WHEN** explicit aliases, contact profiles/bindings, copied mention tokens and cached group rosters are projected
- **THEN** canonical routing identities remain distinct from selected bound-name profiles, token keys/account/group scope and original roster evidence remain intact, and display numbers or cached canonical fields never create phone identities
- **AND** confirmed native metadata can refresh the imported name preference while unconfirmed displays cannot displace it

#### Scenario: Identity projection failure or replay

- **WHEN** a projection lacks its bot prerequisite, meets foreign scope/native conflicts, fails during roster persistence, or is repeated after later native updates
- **THEN** invalid/failed projection leaves no partial identity/mention writes and completed replay does not reset later names or rosters; import dispatch remains blocked

#### Scenario: Imported routes and retirement choices

- **WHEN** inbox, dedicated, mirror, muted and ignored routes are projected after the scoped directory
- **THEN** their destinations, saved inboxes, return generations, private-inbox retirement and topic-name caches remain intact without creating a topic or changing permanent account identity
- **AND** retired-topic hashes require exact retained coordinate evidence and remain permanent native reservations; an owned retirement crash stays fenced until its continuation reconciles the original route

#### Scenario: Route projection conflict, failure or replay

- **WHEN** topic ownership or aliases conflict, a retired hash lacks exact evidence, persistence fails, or a completed projection is repeated
- **THEN** conflicts are refused and failed writes roll back together; exact replay preserves later native changes and cannot change the frozen planned provider connection

#### Scenario: Imported completed live source

- **WHEN** a legacy source has explicit completed-delivery evidence and a duplicate live event arrives after import
- **THEN** live text/media relays avoid creating a replacement plan or sending again without inferring completion from partial mappings or uncertain effects
- **AND** device/message-ID colons and account scope remain intact

#### Scenario: Completion evidence meets frozen work or history-all

- **WHEN** an existing frozen native/text plan requires recovery, or the owner explicitly requests history-all copies of a completed live source
- **THEN** original frozen work retains precedence and history uses its separate run/copy scope; the live completion marker neither adopts unknown sends nor suppresses requested fresh history copies

#### Scenario: Imported retained copies and observations

- **WHEN** retained messages and Telegram cleanup observations are projected
- **THEN** private bot scopes, physical copy order, exact integer sequence IDs, original media/format/mention evidence and cleanup state remain intact
- **AND** actual provider receipts and original user observations remain distinct; missing receipts stay explicitly absent, unknown effects are not adopted, and group locations require consistent retained evidence

#### Scenario: Imported copies feed native workflows

- **WHEN** a retained legacy copy is edited, reacted to, revoked or reused as a native media source
- **THEN** original caption/media, formatting, ownership and bot-file scope evidence govern the operation without fabricating a send acknowledgement
- **AND** ungrouped legacy reactions use the newest retained copy, including a newer text-only copy after older media

#### Scenario: Retained-message projection failure or replay

- **WHEN** persistence fails after message/observation writes and sequence advancement, or a completed projection is repeated
- **THEN** failure rolls back rows, self identity and sequence changes together; replay preserves later observations and counters never rewind

#### Scenario: Retained participant identity

- **WHEN** an owner replies `/create` to an imported copy whose original sender/context was retained without a native job snapshot
- **THEN** the participant identity is available for the normal scoped provisioning flow without inventing absent body/quote content
- **AND** unbound legacy display caches remain harmless names; non-person cache identifiers cannot bind contact bots or establish phone identities

#### Scenario: Proven own-account reaction actor

- **WHEN** a confirmed reaction echo returns with an own-account LID proved by group/Status source evidence
- **THEN** the actor can match that echo without guessing a phone alias, while ordinary target/emoji/age/account checks remain enforced
- **AND** an outgoing private message does not prove its recipient is the own-account actor

#### Scenario: Retained latest edit and native precedence

- **WHEN** an imported source has a saved latest edit, including an empty body or explicit empty mentions
- **THEN** source/history normalization uses that edit without creating a synthetic job or provider acknowledgement
- **AND** later native edits supersede its body, inherit unspecified mentions and preserve explicit mention clearing; unordered conflicting alias caches are refused

#### Scenario: Retained edit echoes and revocation markers

- **WHEN** saved outbound edit hashes and revocation markers are projected
- **THEN** only a matching account/chat/message/body edit is suppressed as an echo and explicit true markers suppress future source edits/copies
- **AND** projection does not claim Telegram copies were deleted, schedule new operations or activate the account; failure rolls back all projected state and replay preserves later native edits

#### Scenario: Retained poll selections and reaction resolutions

- **WHEN** selected poll options, custom emoji equivalents and outgoing reaction resolutions are projected
- **THEN** exact option order/text and supported, unsupported or removal choices survive with their original account/bot/event scope
- **AND** native handling reuses the frozen choice without refetching or treating it as a confirmed send; unknown source operations remain unresolved

#### Scenario: Interaction projection rollback or replay

- **WHEN** a projection write fails, an alias-convergent selection conflicts or a completed projection is repeated after native cache updates
- **THEN** failure commits no partial interaction state, conflicts are refused and replay preserves later native selections/equivalents
- **AND** projection never activates the account while remaining journals/assets are pending

#### Scenario: Retained incoming reaction facts

- **WHEN** incoming actor state, represented echoes and confirmed Telegram setting/refusal caches are projected
- **THEN** their opaque hashes resolve only against retained original message/job/copy evidence and every target keeps its source, destination and private bot scope
- **AND** actor order, refusal timing and echoes survive without inventing full-snapshot absence or identities absent from the old applied cache
- **AND** actor aliases within one state follow their shared revision order, while conflicting independent alias caches require evidence rather than an invented ordering

#### Scenario: Original reaction fallback remains unfinished

- **WHEN** a saved destination cache has a pending fallback notice
- **THEN** its original scoped reaction-job reference remains pending until job/notice compatibility is complete, without a new send, fabricated acknowledgement or early activation
- **AND** all projected reaction facts/references roll back on failure; replay preserves later native state, while missing evidence and conflicting alias caches are refused

#### Scenario: Remove a previously refused reaction

- **WHEN** an actor removes a reaction whose native setting was refused and no applied reaction remains
- **THEN** fenced local progress clears the obsolete refusal without a provider call, so a new reaction can retry immediately
- **AND** member refusals still used for master fallback remain cached; cleanup does not invent an applied setting or send acknowledgement

#### Scenario: Rehearse implemented preparation stages together

- **WHEN** an operator runs `legacy-prepare` against an explicitly migrated fresh destination and a frozen archive
- **THEN** the implemented capabilities run in dependency order with original identity/key and fixed planned connection, without provider calls, external asset restoration or activation
- **AND** a failed capability leaves earlier checkpoints inactive and resumable; exact replay preserves their state, redacted failures name the capability and successful preparation still reports not ready

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

### Requirement: Fenced mirror route transitions

The Go implementation SHALL admit manual mirror commands durably and freeze route-transition plans. Route commits SHALL serialize destination ownership and persist complete confirmation plans. Unmirror SHALL reserve its conversation before topic creation, coordinate with active imports, preserve confirmed receipts and paused preferences, and retain ambiguity for review.

#### Scenario: Confirmation plan cannot commit

- **WHEN** a link cannot persist its full confirmation plan
- **THEN** the route change, former-inbox retirement and metadata admission do not partially commit

#### Scenario: Topic creation succeeded before local persistence failed

- **WHEN** an unmirror topic receipt is confirmed but the route move cannot commit
- **THEN** a new consumer reuses the original receipt and destination without creating another topic

#### Scenario: A discarded refusal needs guidance recovery

- **WHEN** a definite topic refusal was discarded and its owner-guidance write failed
- **THEN** retry persists the original guidance without attempting topic creation again

#### Scenario: A return notification meets a later link

- **WHEN** a later link makes an earlier return confirmation obsolete
- **THEN** only definitely unsent notification effects close, completed receipts remain retained and uncertain sends still require review

#### Scenario: An unresolved move meets a new import

- **WHEN** topic creation is pending or uncertain for a reserved conversation
- **THEN** competing route moves and new active history imports wait without changing the original route or creation key

### Requirement: Durable native mirror selection

The Go implementation SHALL preserve native private-group selection, explicit privacy constraints and requested administrator rights. Picker requests SHALL bind account, owner, source route, return generation and deadline. Their consumption SHALL commit with the selected route and complete group/private confirmation plans. Retries SHALL reuse original request IDs, targets and receipts.

#### Scenario: SDK omits a false privacy constraint

- **WHEN** the SDK model would omit the native private-group constraint during JSON encoding
- **THEN** the adapter preserves the explicit false value while retaining the SDK transport and server-side destination validation

#### Scenario: Selection expires before route commit

- **WHEN** an admitted selection is no longer valid before its management job commits
- **THEN** no route change or request consumption commits

#### Scenario: Private confirmation retries after group success

- **WHEN** the group confirmation has a receipt but the private completion is refused or needs local reconciliation
- **THEN** retry retains the private destination and keyboard removal without repeating the group notice or route change

### Requirement: Frozen conversation topic controls

The Go implementation SHALL persist complete topic headers and scoped actions before sending. Header recovery SHALL respect the recorded route and retain receipts. Callbacks SHALL validate owner, bot, account, accessible topic, conversation, operation, generation and expiry before admitting independent management work. Closing obsolete headers SHALL NOT discard uncertain sends.

#### Scenario: Header plan cannot commit

- **WHEN** a new header cannot persist its full plan
- **THEN** its choices, actions and metadata admission do not partially commit

#### Scenario: Scoped action is copied or changed

- **WHEN** a callback is replayed in another topic or its operation/account/generation/deadline no longer matches
- **THEN** acknowledgement does not admit an owner action

#### Scenario: A later route makes a header obsolete

- **WHEN** a prepared header no longer matches its route before recovery
- **THEN** definitely unsent parts close while completed receipts and uncertain-send review remain intact

### Requirement: Fenced metadata text stages

The Go implementation SHALL atomically persist validated source snapshots and frozen metadata text plans, preserving unavailable descriptions and incomplete rosters. Text recovery SHALL reuse original targets, values and receipts, check current identity/route/generation/snapshot scope, and retain unapplied topic names. Completing text stages SHALL NOT imply a completed refresh run or enable partial runtime dispatch.

#### Scenario: Optional description or incomplete roster

- **WHEN** the provider omits a description or returns an incomplete group roster
- **THEN** unavailable descriptions cannot clear Telegram data and incomplete rosters cannot replace stored metadata

#### Scenario: A topic rename has not succeeded

- **WHEN** a source name refresh commits but its topic rename fails
- **THEN** a later request still compares against the unapplied topic name and does not mistake source persistence for provider success

#### Scenario: Metadata plan persistence fails

- **WHEN** the full text plan cannot commit
- **THEN** snapshots, rosters/profiles, route names and topic-name evidence roll back together before any provider mutation

#### Scenario: Partial or obsolete text recovery

- **WHEN** a confirmed setter needs local reconciliation or the recorded route, binding, generation or source snapshot becomes obsolete
- **THEN** recovery retains confirmed receipts without another setter and never retargets old work into the newer scope

### Requirement: Durable metadata picture stages

The Go implementation SHALL preserve unavailable pictures and shared management identity, cache by source content and current Telegram photo ID, and freeze bounded JPEG assets before uploads. Cache recovery SHALL require confirmed upload and scoped native photo-ID observation receipts. Retries SHALL retain original bytes/targets; uncertain uploads SHALL remain reviewable. Stage completion SHALL NOT imply completed refresh orchestration.

#### Scenario: Source URL rotates or picture becomes hidden

- **WHEN** a signed URL changes without changed bytes, or its picture becomes unavailable
- **THEN** matching cached bytes/current photo ID avoid duplicate uploads and unavailable sources preserve pictures/cache evidence

#### Scenario: Cache persistence fails before a manual replacement

- **WHEN** an upload and photo-ID observation are confirmed but cache persistence fails before a later manual avatar change
- **THEN** recovery retains the original observed ID without another upload or associating the old source hash with the later avatar

#### Scenario: Frozen upload retries or becomes uncertain

- **WHEN** a definite refusal retries after source/consumer changes, or the upload response is lost
- **THEN** retries reuse persisted JPEG bytes and uncertain outcomes cannot become cache evidence or an automatic second upload

#### Scenario: Photo plan or scope is invalid

- **WHEN** plan persistence fails, source bytes exceed their budget or the account/claim/route/generation/binding/snapshot becomes obsolete
- **THEN** assets and plans commit atomically before mutation, invalid images cannot replace avatars and old work cannot retarget a new scope

### Requirement: Scoped metadata commands and permission advice

The Go implementation SHALL preserve owner sync scope and atomically admit independent work with frozen acknowledgements. Permission grants SHALL bind account, session, owner, mirror, conversation, generation and the original 24-hour deadline. Native membership advice SHALL preserve legacy status/400 handling and frozen grant/button data. No metadata control SHALL mutate WhatsApp.

#### Scenario: Bound contact bot has no direct route

- **WHEN** its owner requests sync in the bound bot
- **THEN** only that contact is queued without creating a direct route or admitting global work

#### Scenario: Permission grant is expired or copied

- **WHEN** its owner/account/session/group/generation/deadline no longer matches
- **THEN** scoped guidance admits no sync; a saved acknowledgement retry cannot enqueue again or change targets

### Requirement: Evidence-backed metadata completion notices

The Go implementation SHALL seal reports only from terminal account/run/session-bound target jobs with resolved effects. Counts SHALL use confirmed mutation receipts. Complete notice plans SHALL retain original request destinations, content, buttons and expiry, deduplicate recipients and retry only unconfirmed packets. Quiet cron and manual-waiter semantics SHALL remain unchanged. Sealed notices SHALL NOT imply implemented run orchestration.

#### Scenario: Target work remains uncertain

- **WHEN** any target is unresolved or has sending/uncertain effects
- **THEN** no completion report, summary record or notice plan commits

#### Scenario: Coalesced private recipients overlap chat IDs

- **WHEN** two bots notify the same private owner and only one packet has a receipt
- **THEN** recovery keeps distinct bot scopes and sends only the unconfirmed packet

#### Scenario: Unchanged cron run has a manual waiter

- **WHEN** no fields, warnings or canonical roster/admin state changed
- **THEN** cron stays quiet for its default notifier while manual waiters still receive their frozen report
