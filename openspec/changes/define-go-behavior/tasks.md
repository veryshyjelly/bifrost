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
- [ ] Integrate old-topic retirement, other controls, legacy-state import and full runtime startup.
