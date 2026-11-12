# Tasks

## 1. Baseline documentation

- [x] Create port/golang from current HEAD and initialize repository-local OpenSpec.
- [x] Record eleven capability baselines, source/fixture evidence and verification limits.
- [x] Record required provider clients, latest release policy, PostgreSQL and Docker Compose target constraints.
- [x] Run the Bun regression baseline: 263 passed, 11 skipped, 0 failed.

## 2. Desired behavior review with the owner

- [x] Review routing, member bots, topics, group linking, unmirroring and authorization; preserve route types and intentional old-topic deletion.
- [x] Review sends, replies, edits, formatting, mentions, reactions and deletion.
- [x] Review attachments, stickers, manual history and Status feeds.
- [x] Review ordering, retries, deduplication, uncertainty, restart recovery and read receipts.
- [x] Review ingress, configuration, sync, scheduling, cleanup and identity transfer.
- [x] Add agreed behavior deltas and acceptance scenarios; resolve all open review questions.

## 3. Validation and handoff

- [x] Validate all specs and change artifacts with OpenSpec strict validation and audit tracked and new documentation files for private data.
- [x] Record preliminary SDK feature coverage and integration verification needs, including explicit empty-caption serialization.
- [ ] During the separate Go implementation design, verify every required operation against exact latest released SDK/gateway/API versions, including conditional caption clearing.
- [x] Confirm the desired behavior contract is complete before beginning a separate Go implementation design.

## 4. Go implementation progress

- [x] Add pinned pgx/Goose dependencies and sqlc tool/query generation.
- [x] Implement permanent identity/key binding and legacy-compatible AES-GCM token sealing.
- [x] Implement durable admission, stream-head claims, priority, renewable fenced leases and explicit recovery.
- [x] Preserve frozen effect requests, immutable successful receipts and explicit uncertain-send review.
- [x] Implement private configuration generation, explicit migration CLI, diagnostics and admin review HTTP.
- [x] Include the complete Compose service definitions, one PostgreSQL instance with isolated app databases/users, OpenWA/Redis and the local Telegram API.
- [x] Verify real PostgreSQL concurrency, recovery and cross-database permission denial with disposable test state.
- [x] Build Go/OpenWA images and smoke-test shared PostgreSQL, Redis, OpenWA and diagnostic Bifrost in a disposable Compose project.
- [ ] Build/test the pinned official Telegram API service with the provider-adapter slice; it was excluded from this smoke test.
- [x] Implement initial provider adapters and authenticated durable webhook admission before enabling any relaying.
- [ ] Port conversation routing, controls and remaining capability scenarios.
- [ ] Implement and test the explicit SQLite state importer and live cutover procedure.

## 5. Provider SDK and live diagnostic progress

- [x] Resolve and pin OpenWA Go SDK v0.11.0 and go-telegram/bot v1.28.0 independently from the gateway release.
- [x] Add safe SDK transports, native text/quote/edit clients, session/stored-message reads and explicit Telegram getMe diagnostics.
- [x] Verify no hidden mutation retries, no credential redirects, safe error redaction and retry/uncertainty classification through HTTP fixtures.
- [x] Exercise the owner-authorized primary group/DM and second-session group reply; record private stable keys and receipts outside Git.
- [x] Verify a completed same-key send replay returns the identical message ID.
- [x] Update the integrated gateway build pin to stable OpenWA v0.30.0, build it from source and validate the disposable shared-PostgreSQL/Redis stack startup.
- [x] Authenticate the Telegram SDK against the existing local API using read-only getMe.
- [x] Assess the healthy live OpenWA layout and record the state-preserving migration contract; no move is needed for SDK validation.
- [x] Implement opt-in durable OpenWA/Telegram/management admission, cleanup observations and restart deduplication without registering callbacks.
- [ ] Implement callback registration, dispatch and remaining provider operations before switching the live bridge.

## Worker and initial conversation implementation

- [x] Run fenced claims with bounded concurrency, renewable leases, conservative effect handling and safe shutdown.
- [x] Preserve original requests and completed receipts across partial delivery and local persistence failures.
- [x] Explicitly close handled, definitely undelivered effect branches without bypassing uncertain-send review.
- [x] Implement initial inbox creation, atomic route/management-notice persistence and private/mirror target selection.
- [x] Preserve one-bot-per-contact binding and queue inbox retirement independently when a bound bot is started.
- [ ] Connect full workflow handlers, verified aliases, routing transitions, rendering, provider dispatch and callbacks before starting the worker in the runtime.

## Text relay implementation

- [x] Port literal/native-entity sender presentation and UTF-16 splitting against legacy runtime golden fixtures.
- [x] Persist complete text delivery plans before sending, retaining original targets, entities, quotes, packet keys and receipts across retries.
- [x] Record scoped mappings and cleanup observations after confirmed receipts; isolate private bot IDs and management topics.
- [x] Suppress outgoing echoes using both message mappings and confirmed effect receipts; defer own events during unresolved sends.
- [x] Queue read receipts independently after successful outgoing sends and preserve owner pause decisions for definitely unsent parts.
- [ ] Integrate verified identities/mentions, command and media dispatch, edits, repair/cleanup and full worker startup before enabling runtime forwarding.

## Identity and mention implementation

- [x] Store account-scoped verified aliases, readable contact profiles, mention tokens and fresh roster snapshots behind fenced writes.
- [x] Serialize alias and bot binding changes to reject conflicting canonical identities, including concurrent registration.
- [x] Render metadata-backed incoming tags as native present-bot mentions or profile/WhatsApp links with UTF-16 offsets.
- [x] Resolve outgoing native name entities, registered usernames and copied tokens, excluding code, URLs and email spans.
- [x] Validate outgoing group membership against a roster younger than five minutes; allow labeled cached data only for picker display.
- [x] Freeze resolved mentions inside text relay plans and send native WhatsApp mention IDs through the released SDK.
- [x] Implement webhook-to-canonical-message resolution and independently verified self identity for owner links.
- [x] Implement the mention picker commands/callbacks and owner-facing refusal notices.
- [ ] Integrate mentions into attachment captions, edits and history delivery.

## Source normalization and dispatcher implementation

- [x] Atomically admit native source snapshots, verified identity evidence and stable canonical conversation queues.
- [x] Preserve established LID conversations when verified phone aliases arrive, without rewriting routes, receipts or stable delivery keys.
- [x] Fence older unsnapshotted job recovery and stream transitions against claims, preserving IDs and retry budgets.
- [x] Connect native WhatsApp text, Telegram owner text and independent management read events through the worker dispatcher registry.
- [x] Resolve owner links from independently verified session identity; skip new identity/group lookups for prepared plans, echoes and suppressed routes.
- [x] Exercise authenticated admission through PostgreSQL, the fenced worker and both native provider SDKs with isolated fixtures.
- [ ] Install remaining workflow handlers, automatic cleanup, managed-bot credentials and importer before final runtime worker composition.

## Mention picker and control reply implementation

- [x] Preserve native copy buttons, ten-member pagination, cached-roster labels, Unicode search and locale sorting against legacy Node golden fixtures.
- [x] Validate callback owner, permanent account, bot, effective topic, conversation and expiry, including inaccessible messages and whole-group reply threads.
- [x] Freeze reply packets, callback acknowledgements and scoped navigation actions before sending; repair local observation failures without duplicate sends.
- [x] Preserve command addressing, captioned commands and ignored command/former-command edits.
- [x] Send original mention refusal notices without WhatsApp sends/reads; prevent notice retry from reconsidering the rejected message.
- [x] Persist command/reply expiry metadata and legacy 60/600-second defaults without expiring conversation/protected messages.
- [x] Implement automatic notice expiry/deletion and scoped clear commands.
- [ ] Implement remaining owner controls and menus before full runtime composition.

## Telegram cleanup implementation

- [x] Schedule notice expiry atomically with command/reply recording and backfill existing timed notices without expiring archives.
- [x] Freeze manual-clear membership and original bot/chat/topic, protecting later commits even below the cutoff sequence.
- [x] Preserve the 100-item bulk path, individual fallback, age/protected skips and permission-refusal notices.
- [x] Journal idempotent deletion targets/outcomes separately while retaining fencing and uncertain message-send review.
- [x] Preserve mappings and deleted-copy markers, including relay receipt-before-mapping and unresolved-send guards.
- [x] Exercise authenticated cleanup commands, restart persistence, native SDK calls and local/remote failure recovery with isolated PostgreSQL.
- [x] Implement independent former-inbox retirement with shared-topic safeguards and frozen deletion recovery.
- [ ] Integrate other controls, legacy-state import and full runtime startup.

## Former inbox retirement implementation

- [x] Discover current, empty remembered and mapped former management topics without deleting General, mirror messages or private-bot copies.
- [x] Refuse shared topics and serialize candidate admission with route writes and frozen relay preparation.
- [x] Wait for incomplete relays, reserve original topic IDs and protect fresh restorations from deletion retries.
- [x] Use native idempotent topic deletion with durable outcomes, fenced writes and independent permission/refusal recovery.
- [x] Verify activation, provider failures, database reopening, rollback, concurrent route admission and lease fencing with isolated PostgreSQL/SDK fixtures.
- [ ] Compose remaining owner commands, mirror transitions, history/import and full runtime startup.

## Conversation owner command implementation

- [x] Preserve `/keep`, `/mute`, `/ignore` and `/read` command addressing, caption handling, ignored edits and original route/mode rules.
- [x] Resolve member-bot private commands through verified bindings and stable conversation keys without creating missing routes.
- [x] Admit independent management jobs atomically with source command plans and expiry scheduling, rejecting foreign mapped replies.
- [x] Commit preference changes with their frozen master-bot confirmation plans; preserve later decisions during notice recovery.
- [x] Reuse independent read jobs without automatic reads from preference commands or incoming relays.
- [x] Exercise native SDK dispatch, rollback, notice uncertainty, database reopening, scoped authorization and fencing with isolated PostgreSQL.
- [ ] Implement startup/help and menus, remaining controls, mirror transitions, managed credentials, history/import and final runtime composition.

## Start help and command menu implementation

- [x] Preserve ordinary master/member Start guidance, command addressing, ignored edits and interactive-help lifetimes.
- [x] Commit member activation, missing direct-route creation, cleanup admission and the original help plan together.
- [x] Delegate mirror and sync Start links without consuming them as ordinary help.
- [x] Record explicit validated migration eligibility and deduplicate startup menu jobs for migrated registered bots.
- [x] Preserve legacy ordered command menus, owner-only management scope and the native commands menu button.
- [x] Freeze idempotent settings and journal confirmed progress without changing message-send uncertainty rules.
- [x] Verify native settings, partial retries, lost responses, database reopening, rollback and fencing through isolated PostgreSQL/SDK fixtures.
- [ ] Implement native credential migration/import and full runtime startup, status/remaining controls, mirror transitions and remaining delivery workflows.

## Manual mirror transition implementation

- [x] Preserve native manual `/link` and `/unmirror` command addressing, owner checks, guidance and independent management admission.
- [x] Validate private destinations and master administrator identity through the current native Telegram SDK.
- [x] Serialize destination ownership and commit link routing, confirmations, retirement and metadata admission together.
- [x] Freeze unmirror topic creation, coordinate active history states and retain persistent guards through unresolved outcomes.
- [x] Reconcile confirmed topic receipts and discarded refusals across failed local writes and new consumers.
- [x] Preserve paused modes, old mirror archives/mappings and return generations; close only definitely unsent obsolete notices.
- [x] Exercise native SDK dispatch, concurrency, rollback, privacy, uncertainty and stale/ABA recovery with isolated PostgreSQL.
- [ ] Implement header-initiated history/contact provisioning handlers and final runtime composition.
- [ ] Implement explicit transition reconciliation, full history/import and final runtime composition before live cutover.

## Private group picker and topic control implementation

- [x] Preserve `/group`, mirror Start links, native reply keyboards, administrator rights and explicit private-group selection constraints.
- [x] Persist account/owner/route/generation/deadline-bound choices and numeric request IDs with frozen reply plans.
- [x] Revalidate and consume selection requests atomically with route changes and both confirmation destinations.
- [x] Restore original group/contact topic headers and scoped callback actions with durable metadata admission.
- [x] Preserve per-packet receipts, keyboard removal, configured lifetimes and uncertain-send review across retries/restart.
- [x] Verify source authorization, scope/expiry rejection, rollback, native wire serialization and obsolete-header recovery with isolated PostgreSQL/SDK fixtures.
- [ ] Execute history and contact provisioning handlers; complete transition reconciliation/import and final runtime composition.

## Metadata snapshot and text stages

- [x] Preserve optional group descriptions and validated participant lists through maintained SDK reads.
- [x] Commit session-scoped snapshots, rosters/profiles, route names and complete text plans atomically under account/claim fencing.
- [x] Freeze inbox/mirror text and existing contact-bot name/description updates, retaining independent receipts across retries and reopened consumers.
- [x] Reject obsolete routes, generations, bindings, snapshots and public mirror targets; preserve retired topics and unapplied topic names.
- [x] Verify unavailable/empty descriptions, LID identity, rollback, safe setter retries and receipt reconciliation with isolated PostgreSQL/SDK fixtures.
- [x] Complete run admission/outcome collection, shared-contact orchestration and persistent cron in isolated worker fixtures.

## Metadata picture stages

- [x] Use native SDK picture lookups and group/static bot-photo uploads while preserving hidden pictures and shared management identity.
- [x] Bound CDN downloads, decode supported images, pin current image codecs and freeze JPEG bytes with complete photo plans before mutation.
- [x] Scope content-hash caches by account/session/identity/destination/generation and verify the current native photo ID before reuse.
- [x] Persist independent upload and photo-ID observation receipts, reconcile failed cache writes and preserve uncertain outcomes for review.
- [x] Verify URL rotation, manual changes, hidden sources, partial/uncertain delivery, rollback, obsolete scope, restart and account/claim fencing with isolated PostgreSQL/SDK fixtures.
- [x] Compose picture stages with run orchestration/coalescing, target outcome collection and scheduling in isolated worker fixtures.

## Metadata commands, permission advice and sealed reports

- [x] Preserve owner `/sync` scope, bound contact-bot commands without direct routes, argument/addressing rules and independent durable management admission.
- [x] Freeze 24-hour account/session/owner/group/generation permission grants and validate native group Start links before scoped sync admission.
- [x] Persist native missing-bot advice with scoped grants; retain canonical roster/admin-change evidence without label/alias false positives.
- [x] Require terminal account/run/session-bound targets and resolved effects before sealing report statistics, text, buttons, recipients and expiry evidence.
- [x] Preserve quiet cron/manual waiters, receipt-based mutation counts, warning/button limits, independent recipient receipts and uncertain-send review.
- [x] Verify native admission, scope/expiry rejection, rollback, new consumers, partial reports and coalesced recipient recovery with isolated PostgreSQL/SDK fixtures.
- [x] Implement run admission, terminal/partial target outcome collection and retry budgets, target/contact fan-out, source serialization/coalescing and persistent cron in isolated worker fixtures.

## Durable metadata runs and timezone scheduling

- [x] Commit run admission, original request membership and start jobs atomically; replay request IDs and coalesce account/session-scoped active runs.
- [x] Discover active global groups and registered contact bots, fan out frozen group rosters and deduplicate canonical/shared contact destinations.
- [x] Continue independent fields after known refusals, preserve partial target outcomes and apply the four-attempt provider retry budget without swallowing local persistence errors.
- [x] Serialize metadata source streams and predecessor aliases/bindings; preserve uncertain effects as barriers while independent chat work continues.
- [x] Seal complete target/request membership atomically with final report admission, retaining independent notification recovery.
- [x] Preserve numeric five-field cron, timezone/DST and configured/off gates with persistent minute deduplication across concurrent/reopened consumers.
- [x] Verify admission rollback/concurrency, full native owner-command execution, confirmed-mutation outcome recovery and partial advice/counts with isolated PostgreSQL/SDK fixtures.
- [ ] Compose the scheduler and metadata handlers into the complete runtime after remaining workflows and legacy import are ready.

## Managed credentials and contact provisioning

- [x] Resolve current SDK releases and implement native managed-bot events/token retrieval with encrypted persistent credentials.
- [x] Journal explicit hosted logout, local identity verification, authenticated webhook settings and menu admission without dropping pending updates.
- [x] Preserve uncertain logout review, confirmed migration evidence and registration/settings recovery.
- [x] Preserve contact creation at capacity, immutable group-participant selection, scoped single-use binding and conditional inbox retirement.
- [x] Verify native SDK requests, PostgreSQL rollback, encryption, routing preservation and owner/destination/choice guards in isolated fixtures.
- [ ] Compose credential/provisioning handlers and imported bot evidence into the complete runtime.

## History and native attachment capabilities

- [x] Implement scoped single-use history choices with independent prompt cleanup, complete frozen batches and conversation-specific live holds.
- [x] Implement destination-scoped skipping, fresh import quotes, retained daily headings and known partial live-plan adoption without rekeying effects.
- [x] Implement native incoming/outgoing attachment, location, contact and poll packets with bounded authenticated retrieval, frozen media references and separate read receipts.
- [x] Implement validated WhatsApp/Telegram sticker conversion, transparent native Go TGS rendering and untouched original fallbacks.
- [x] Exercise native SDK/PostgreSQL retry, mapping, partial/unknown delivery and codec/container fixtures in isolation.
- [ ] Integrate these capabilities with the complete runtime, remaining change workflows and legacy import acceptance suite.

## Mapped edits

- [x] Implement owner mapped text/caption edits with conditional native empty-caption requests and independent refusal notices.
- [x] Implement retained-copy source edits using frozen presentation/entities, canonical streams, out-of-order evidence and confirmed edit echo suppression.
- [x] Validate known retry freezing, missing/cleared destination preservation and nonblocking refusal notices in native SDK/PostgreSQL fixtures.
- [ ] Complete delivery repair, native reactions, poll controls, revocation choices and final runtime/import wiring.

## Native poll controls

- [x] Validate scoped, expiring poll callbacks against retained native delivery receipts and current binding/route.
- [x] Serialize per-poll toggles/replacements using exact option texts and confirmed selections.
- [x] Preserve previous votes on definitive refusals and use a frozen idempotent journal for transport/crash recovery.
- [x] Queue passive attributed source-vote notices without WhatsApp read receipts; validate native payload and worker fixtures.
- [ ] Integrate poll controls with the complete runtime and legacy action import.

## Native reaction projection

- [x] Preserve per-actor snapshots, removals, equivalent Unicode identities and confirmed owner echoes.
- [x] Project onto retained fresh copies/attachments using member bots, scoped private IDs, refusal caching and attributed master fallbacks.
- [x] Validate and freeze custom-emoji equivalents without clearing unresolved reactions; deduplicate confirmed equivalent changes.
- [x] Exercise native adapter payloads, safe setter retries, crash recovery, missing targets and independent fallback notices in isolated fixtures.
- [ ] Complete explicit /react and /delete commands, source revocation choices and runtime/import wiring.

## Source revocation choices

- [x] Persist source tombstones before destination work and prevent late live/history resurrection.
- [x] Implement owner-scoped inline/reply Delete/Keep controls with exact destination/topic/private-bot scope and permanent-account identity.
- [x] Preserve original content on Keep, expiry, stale conflicting clicks and deletion refusal; delete all parts only in the selected destination.
- [x] Exercise native markup, prompt refusal/cleanup, passive expiry and fenced idempotent delete-cursor recovery in isolated fixtures.
- [ ] Integrate revocation controls with complete runtime composition and legacy task import.

## Explicit source actions

- [x] Admit owner /react and /delete only from a mapped reply in the current conversation/topic/private-bot scope; preserve edited-command and argument behavior.
- [x] Preserve the authenticated direct management action surface with unique originating-job admission and frozen native requests.
- [x] Use idempotent source mutation progress, independent refusal notices and authoritative revoke evidence before Telegram cleanup.
- [x] Validate native payloads, replay, permission refusal and crash recovery through isolated SDK/PostgreSQL fixtures.
- [ ] Complete runtime/import composition.

## Guarded route repair

- [x] Restore missing management topics and fall back from forbidden member bots only before any confirmed or uncertain packet.
- [x] Preserve resolved mentions, frozen media/conversion choices, route modes and original text-plan receipts.
- [x] Fence restoration with route generations, directory locks, permanent reservations and durable topic-creation guards.
- [x] Validate partial-delivery refusal, stale routes, unknown creation, confirmed-topic crash recovery and unexposed poll controls in isolated fixtures.
- [ ] Integrate complete runtime and explicit abandoned-transition reconciliation.

## Passive Status archive

- [x] Archive native Status text/media with linked authors and native posting times through authenticated local retrieval.
- [x] Freeze refresh batches and deduplicate webhook races without conversation creation, source publication or read/view receipts.
- [x] Preserve archives after source expiry and persist skipped posts across unmute/refresh.
- [x] Implement alias-scoped author preferences, ten-person pagination, expiring presented controls and stale-revision refusal.
- [x] Exercise topic uncertainty, frozen refreshes, callback/edit recovery, passive topic guidance and explicit migration startup gates in isolated fixtures.
- [ ] Compose runtime webhook subscription, startup refresh and legacy Status state import.

## Operational controls and notices

- [x] Preserve owner /status snapshots, legacy latest-sync fields, command expiry and native /inbox forum guidance.
- [x] Admit passive source session/message-failed events as independent literal management notices.
- [x] Commit enabled failure/review notice admission with source job-state changes, preserving uncertainty and preventing recursive notices.
- [x] Validate frozen replies, native reads, atomic rollback, diagnostic disablement and crash recovery in isolated fixtures.
- [ ] Complete explicit topic reconciliation and runtime/import composition.

## Explicit topic reconciliation

- [x] Inspect supported frozen topic requests without provider calls or identity changes.
- [x] Adopt an explicitly confirmed topic or confirm absence with exact account/request/state evidence and persistent guard checks.
- [x] Preserve completed receipts, unrelated uncertainty, permanent reservations and later owner decisions across audit replay.
- [x] Expose authenticated strict HTTP handling and explicit local CLI inspection/review without redirecting credentials.
- [x] Exercise initial inbox, mirror return, native repair and Status scopes, unprepared effects, audit rollback and crash recovery in isolated fixtures.
- [ ] Integrate legacy pending topic state and complete runtime startup recovery.

## Automatic metadata trigger compatibility

- [x] Preserve new/link/bind report labels through internal durable admission while keeping public management requests as command work.
- [x] Retain first-source coalescing, non-cron management reports and explicit notifier waiters.
- [x] Exercise native worker reports, invalid trigger scopes, public-source overrides and producer regressions in isolated fixtures.

## Legacy archive inspection

- [x] Validate v1 archive checksums, bounded private extraction and complete gzip/tar streams without executing archived code.
- [x] Read all six legacy SQLite tables in a read-only transaction including committed WAL, preserving exact integer IDs and source strings.
- [x] Verify original account/key/credentials and scoped aliases, bot bindings, mappings, jobs and controls; retain unknown-send evidence.
- [x] Add standalone read-only `legacy-validate` CLI and malicious archive/semantic input tests.
- [ ] Complete transactional PostgreSQL import, active journal compatibility and round-trip/recovery acceptance.

## Transactional legacy preparation

- [x] Preserve exact source rows/KV strings and encrypted original configuration in an atomic account/archive checkpoint.
- [x] Reject nonempty/conflicting destinations and concurrent writers, roll back late validation/COPY/projection failures, and make exact replay harmless.
- [x] Protect source evidence and block runtime readiness/job claims until native journal and asset activation is complete.
- [ ] Project and activate every legacy workflow, restore retained assets and complete the explicit import/cutover CLI.

## Legacy Telegram credential projection

- [x] Project original encrypted member credentials, master token, registration names and Start activation without local migration eligibility.
- [x] Preserve scoped hosted-logout proof and require native local API verification before webhook/menu setup.
- [x] Cover projection rollback/replay, changed credentials, cached identity/Start, refused local API checks and native setup recovery in isolated fixtures.

## Legacy identity and mention projection

- [x] Preserve explicit aliases, original profiles, canonical bindings and selected bound-name sources without deriving phone evidence from LID displays.
- [x] Preserve copied token keys/account/group scope and original roster members/timestamps after credential projection.
- [x] Cover prerequisites, conflicts, foreign scopes, atomic rollback, replay and confirmed native metadata name refresh.

## Legacy route and topic reservation projection

- [x] Preserve all route modes/destinations, inbox references, retirement flags, return generations and topic-name caches after identity projection.
- [x] Recover opaque retired-topic hashes only against retained coordinate evidence and preserve owned retirement crashes behind permanent reservations.
- [x] Cover alias/topic conflicts, missing evidence, connection-scoped replay, native reservation reuse guards and whole-transaction rollback.

## Legacy completed live-delivery evidence

- [x] Preserve explicit source completion markers with canonical account/chat/message scope and exact compound-key parsing.
- [x] Prevent duplicate live native/text plans without fabricating receipts or adopting partial/uncertain delivery.
- [x] Preserve frozen-plan precedence and explicit history-all copies; cover projection rollback, scope and replay.
