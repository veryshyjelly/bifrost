# Implementation specification

Status: architecture decisions finalized on 2026-11-10. This is the coding-agent handoff and governing implementation contract. Creating this specification does **not** authorize implementation, real-event capture, live canary execution, cutover or production deployment. Begin those actions only when authorized; do not infer authorization from the finalized design.

## Authority and evidence

Use this specification with the seven investigation documents:

| Document | Purpose |
|---|---|
| [01 Current architecture](01-current-architecture.md) | Runtime, services, operational boundaries and investigation results |
| [02 Behavioral specification](02-behavioral-specification.md) | Workflow contracts, source anchors and test inventory |
| [03 Domain model](03-domain-model.md) | Identity, mappings, transitions and invariants |
| [04 Proposed architecture](04-proposed-architecture.md) | Namespace responsibilities, interop and runtime evidence |
| [05 Migration plan](05-migration-plan.md) | Staging, single-writer cutover and rollback |
| [06 Testing strategy](06-testing-strategy.md) | Deterministic replay, contracts, failure matrix and normalization |
| [07 Open questions](07-open-questions.md) | Historical uncertainties and their resolution status |

The investigated repository defines baseline behavior. Freeze its exact source revision and relevant file hashes before implementation; the investigation documents alone do not freeze Git state. Preserve original tests and an independently runnable oracle. Report deployed-only differences separately after factual inspection; do not change the baseline to match a legacy image or reintroduce n8n.

When older documents prescribe unconditional preservation of quirks, a completely frozen schema, or further architecture interviews, the finalized decisions below supersede that prescription. Older descriptions of actual code remain evidence, not instructions to reproduce a demonstrated defect. Later explicit owner instructions take precedence. Ambiguous semantics require focused escalation with evidence, not a restarted architecture interview.

Continue using evidence categories: implemented and locally verified; implemented but externally uncertain; documented only; not implemented. Distinguish a test pass from provider verification and historical validation from checks repeated in the current candidate. Preserve unresolved verification items as open engineering work.

## Finalized decisions

| ID | Area | Binding decision |
|---|---|---|
| Q1 | Source of truth | A: investigated repository baseline |
| Q2 | Verification | B: synthetic tests, real-event recordings and controlled live canary |
| Q3 | Collection | A: real events only from dedicated test conversations |
| Q4 | Philosophy | C: correctness first while preserving intended functionality |
| Q5 | Live validation | B: dedicated conversations with an acceptance checklist |
| Q6 | Defect definition | B: violation of documented intent, security guarantees or domain invariants |
| Q7 | Persistence | B: backward-compatible schema extensions only |
| Q8 | Observable corrections | A: explicit exception register and regression tests |
| Q9 | Canary failures | C: continue with monitoring unless data integrity or account security is at risk; delivery safety gates still bind release |
| Q10 | Fix scope | B: critical and migration-relevant defects; defer unrelated noncritical issues |
| Q11 | Correction authority | B: after implementation is authorized, agent may fix demonstrated violations; escalate ambiguous semantics |
| Q12 | Rollback | A: preserve compatibility; unresolved critical conflicts block release |

Duplicate sends, incorrect routing and ambiguous delivery receipts must satisfy safety acceptance gates even if continued canary observation is permitted. Monitoring is not a waiver or release approval.

## Scope and architectural requirements

Deliver an idiomatic ClojureScript rewrite using nbb exclusively on Bun. Main application entry must run with `bunx --bun nbb src/main.cljs` when supplied equivalent configuration. Pin tested nbb and Bun versions; preinstall frozen npm dependencies so service startup does not resolve an unpinned package over the network. Preserve explicit local bridge.env loading and Compose environment behavior. Assert Bun execution for application and application worker processes; no Node fallback, JVM Babashka or core.async.

Use functions, immutable maps, explicit dependency maps and JavaScript Promises or bundled Promesa. Extract pure formatting, parsing, identity resolution rules, routing decisions and state-transition validation from I/O orchestration. Keep provider-dependent membership/roster checks explicit. Use JS objects at wire/storage boundaries and preserve absent/null fields, numeric conversions, UTF-16 entity offsets and durable JSON/hash input bytes.

Retain one paced worker and existing stream ordering, retry classification, target snapshots and effect-ledger semantics. SQLite remains durable truth; local atoms hold busy/timer state only. Do not hold a transaction across asynchronous operations. Do not replace classes with another object hierarchy or add a framework, broker, ORM or services. Namespace names in document 04 are a dependency map; create boundaries when justified by actual responsibilities.

Retain the external pinned OpenWA session REST/event contract, local Telegram Bot API, official Managed Bots creation/events/token retrieval and explicit binding, existing media/sticker tools and isolated codec subprocesses. No BotFather automation, MTProto or manual token substitution. Preserve system codec/browser dependencies; small browser-executed JS resources at the Puppeteer boundary are acceptable where interpreted functions cannot execute in the page. They must have fixture-based parity evidence.

No new product features. Do not automatically implement behavior absent from the original code, including forwarding admitted but unhandled update types or automatic group membership changes. Preserve legacy input/checkpoint compatibility needed for recovery.

## Durable and security invariants

- Preserve BIFROST_ACCOUNT_ID and the distinction between stable account and transport session. Keep aliases conflict-safe and contact/group/author identities distinct.
- Preserve existing job deduplication keys, effect keys, message mappings, receipt results and account-scoped hashes. A done effect must not resend; a sending effect interrupted by crash becomes uncertain.
- Never automatically replay an uncertain send. Retain explicit review and confirmation of no delivery, stable outgoing idempotency headers, and same-stream blocking behind failed/uncertain predecessors.
- Preserve private Telegram bot scope, exact topic matching for topic routes, whole-group handling, cross-conversation quote rejection and owner authorization.
- Keep encrypted credential layout and existing key material compatible. Do not include decrypted tokens, auth headers, token-bearing URLs, private session material or production database contents in logs, fixtures or documentation.
- Preserve raw-byte webhook authentication, size caps, safe provider-error classification, media path containment, authenticated media endpoints and private file permissions.
- Support reopening and continuing old jobs, partial deliveries and workflow checkpoints. Extra schema/state may not make old-code recovery unsafe.

Any intentional correction affecting one of these invariants must demonstrate that the invariant is strengthened or maintained, with current-state rollback evidence. If it cannot, block release and escalate the conflict.

## Correctness exceptions

Parity is expected unless a deviation is registered. A suspicious implementation is an investigation candidate, not proof of a defect. Investigate the three known candidates—keep routing, split-message edits and generic callback validation—against source, tests, documented intent and domain/security invariants. Do not preapprove any particular correction.

When authorized implementation reveals a critical or migration-relevant demonstrated violation, the agent may correct it under Q11. For unrelated noncritical defects, record and defer. For competing plausible meanings or uncertain rollback semantics, present the concrete evidence, proposed correction, alternatives and impact for owner decision before changing dependent behavior. Do independent work while waiting.

Create an exception register under `docs/rewrite/` when the first candidate is evaluated. Use a stable ID and these fields for every intentional observable change:

| Field | Required evidence |
|---|---|
| ID / status | Candidate, demonstrated, corrected, verified, deferred, or escalated |
| Reference | Baseline behavior, source revision, exact source/test locations and reproducer |
| Defect | Specific violated documented contract, security guarantee or domain invariant; explain why it applies |
| Correction | Exact new user/API/state behavior, including deliberate differences from oracle |
| Risk | Routing, delivery, persistence, security and compatibility effects |
| Verification | Baseline failing characterization, candidate regression fixtures, replay results and rollback checks |
| Approval | Autonomous under Q11 with rationale, or explicit owner decision/reference for ambiguous semantics |

A label such as “cleanup” or “idiomatic” does not justify observable divergence. Record an exact expected delta; compare unaffected traces/state strictly. Regression tests must verify the contract, not just mirror the implementation. Updating goldens alone is not proof. No unresolved critical exception or incompatible persistence correction may pass release gates.

## Execution sequence and acceptance gates

At handoff all eight gates below remain **pending**. Prior probes contribute evidence but do not complete a target-runtime gate. Once implementation is authorized, execute progressively in an isolated candidate branch/tree, keeping the baseline runnable. Preserve user work. Do not deploy as a side effect of tests or setup.

Maintain an evidence ledger under `docs/rewrite/` as execution proceeds. Each gate records status, source/candidate revisions, runtime/image/dependency versions or hashes, command, environment, timestamp, outcome, skips, artifact location and unresolved items. Use pass, fail, blocked or pending; a skipped or mocked external check is not a pass for the real capability.

### Gate 1: freeze baseline and oracle

Record Git revision plus hashes for relevant uncommitted baseline files; do not silently include new candidate changes in the oracle. Record dependency lockfiles, pinned provider/image versions and original test results. Build a coverage matrix from document 02 mapping each significant workflow and failure/recovery path to baseline evidence and planned candidate fixtures. Include legacy jobs, checkpoint versions, ignored events and operational tooling.

Produce deterministic synthetic fixtures before comparison. Run the original against the fixture scripts with network disabled. Acceptance: reproducible baseline traces/state, complete inventory mapping, no secret-bearing artifacts, and precise distinction between observed baseline behavior and historical deployment claims.

### Gate 2: runtime compatibility

Run pinned nbb exclusively on pinned Bun in the actual target Linux image/architecture. Verify namespace/npm imports, SQLite, AES-GCM, fetch, streams, multipart/openAsBlob, filesystem containment, child processes/process groups, resource resolution and browser JS boundaries. Verify the exact main command and worker invocation, signal handlers, normal/forced draining, timer behavior and prolonged synthetic operation. The existing [offline probe](experiments/runtime-probe.cljs) is a starting point.

Acceptance: every required deployed path has executable evidence; module loading, codec startup and teardown work without Node/JVM fallback. Any incompatibility has a verified solution within the settled runtime. If no such solution exists, report a release blocker without substituting a runtime.

### Gate 3: persistence compatibility and pure rules

Implement storage and deterministic transforms progressively. Preserve existing tables, keys, JSON conventions and cipher layout. Schema extensions must be additive and justified; do not drop/rename/reinterpret old fields or discard old receipts. Merely being additive is insufficient: verify that old code safely handles new states, jobs and checkpoints.

Test old-write/new-read, new-write/old-read, and old-code continuation after candidate writes using synthetic file databases and private consistent clones when separately authorized. Cover aliases, identity mismatch, queue ordering/due/priority, encryption/tamper behavior, transactions, mappings and old exports. Verify pure formatting/routing rules against oracle, except approved exceptions.

Acceptance: both versions can safely reopen and process the current candidate-written state, not only read its schema. Token comparisons use digests or synthetic plaintext; never print real credentials. Unresolved critical rollback conflicts block release.

### Gate 4: transport equivalence and core relay

Implement adapters and ingress before broad workflows. Compare exact raw-body auth, accepted/ignored/rejected events, OpenWA paths/headers/idempotency/body and Telegram JSON/multipart bytes/entities. Script definite refusals, 429 retry delays, read failures, connection refusal, timeout after acceptance, malformed responses and missing message IDs. Keep safe reason codes and sanitized errors.

Implement text/private/group/reply routing, frozen targets, effects and receipt mappings, then media classes. Acceptance: ordered external effects and durable state match the oracle or individually registered corrections; no test/replay can accidentally call a real service. A failed read receipt or cleanup must never replay a send.

### Gate 5: complete behavioral replay

Complete Managed Bots registration/refresh/binding/readiness; group picker/link/unmirror/retirement; commands/menus/controls/TTL/clear; mentions; edits/reactions/revocations/polls; history; Status; metadata and operational scripts. Use document 02 as the exhaustive inventory rather than treating this list as a reduced scope.

Replay original and candidate independently from identical initial state, clock/random inputs and scripted API responses. Compare per-transition state, user-visible text/entities/buttons, effect order/keys/results, selected jobs and routing scopes. Exercise partial outcomes and repeated/stale controls. Normalize only nondeterministic representation, retaining identity relationships and safety semantics as described in document 06.

Real recordings come only from dedicated authorized test conversations. Filter at collection, not merely before committing files. If a provider batch contains unrelated content and cannot be filtered safely at source, use synthetic fixtures or a separately isolated test identity; do not collect unrelated conversations to obtain coverage. Keep raw recordings private and publish only sanitized fixtures. Acceptance: complete inventory coverage and zero unexplained divergence; every intentional change has verified exception evidence.

### Gate 6: failure and recovery

Inject crashes before and after enqueue/claim, effect sending, remote acceptance, receipt persistence, mapping, completion and workflow checkpoints. Test both graceful and forced shutdown. Verify sending→uncertain, running→pending, done receipt reuse, stream blocking, explicit review scoping and retry/skip. Exercise route/binding changes during partial retry and legacy recovery.

Acceptance: no duplicate send or cross-route delivery in modeled crash cases; uncertain outcomes remain blocked until explicit review; other streams and independent cleanup/read jobs recover as specified. The candidate can resume actual persisted workflow shapes and old code can safely resume after candidate shutdown.

### Gate 7: codecs, deployment and rollback rehearsal

Run default, sticker/codec and Docker transfer/restore tests in target Linux environment. Verify subprocess timeout kills descendants and removes intermediates; compare format/alpha/timing/duration/size, byte-stable frozen retries and document fallbacks. Exercise volume permissions/shared Telegram media and private config/identity preservation.

Run a sustained synthetic soak covering daily media cleanup, cron, expiring controls, realistic attachment mix and backlog recovery. A real 24-hour observation plus accelerated boundary checks is the initial plan; record workload, elapsed time, RSS/event-loop/latency/queue results and assessment rather than claiming an unsupported performance target. Unexplained unbounded resource growth or failure to recover backlog blocks acceptance.

Rehearse candidate writes followed by old-image restart on the **latest** database and same volumes. Inspect actual deployment revision/configuration read-only before cutover when access is authorized, and list deployed-only differences separately. Acceptance: default and opt-in checks pass without unexplained skips for required capabilities, runtime/codec gates complete, and a safe single-writer rollback is demonstrated.

### Gate 8: controlled canary

Prepare a concrete reviewable canary plan before asking for execution authorization: candidate/image hashes, dedicated account/contact/group identifiers held privately, scope, duration, operator, capture filters, checklist, stop conditions and rollback commands. Dedicated test capture/canary are selected verification methods, not blanket permission to use production accounts now. Do not run setup, change webhooks, send messages or provision bots before execution authorization.

Use one live bridge per identity. If a dedicated test conversation uses the same accounts as the existing bridge, stop the old writer first even when the candidate has a separate database. An independent test identity must also remain single-writer. Offline comparison uses fake APIs exclusively.

Minimum acceptance checklist:

- Unknown dedicated test contact gets one inbox topic; duplicate event does not duplicate delivery.
- Official Managed Bot create event/token refresh, explicit correct-contact binding, private Start/readiness and inbox retirement work.
- Private and mirrored-group sends/replies/quotes, owner filtering, bot presence/absence and fallback select the correct chat, thread and bot.
- Representative text/entities/mentions, each required media class and codec fallback retain contents and mappings.
- Telegram/WhatsApp edits, reactions/removals, revocations/Keep/Delete and poll controls behave as specified.
- History skip/all/cancel, retries, Status refresh/mute/read-only behavior, metadata and TTL/clear controls preserve scope.
- Controlled restart retains receipts, pending work and identity; duplicate/ambiguous delivery is reviewed safely. Destructive crash/transport simulations remain offline unless specifically approved.
- No unrelated conversation is captured, no secret is exposed, and no second writer exists. Latest-state rollback remains available.

For each check record expected/actual result and evidence. A failed harmless presentation/control check may continue under monitoring with a recorded outcome. A credible integrity/security risk—duplicate send, wrong recipient/account, lost/corrupt receipt, secret exposure—requires stopping affected activity, preserving evidence and reconciling state. Ambiguous send outcomes must remain blocked for review; do not retry merely to continue the checklist. Do not widen canary scope to compensate for failed checks. All required checks and critical risks must be resolved before release.

## Cutover and rollback contract

Canary completion does not authorize production release. Prepare a concrete cutover report for approval: all gate evidence, registered corrections, compatibility results, remaining limitations, actual deployment differences, maintenance sequence, latest-state backup and rollback rehearsal. Do not relax acceptance gates independently.

For an authorized cutover, stop the old writer, confirm termination, preserve consistent current SQLite/WAL, encryption configuration, media and Telegram volumes, and retain the matching old image. Keep account identity and callback behavior; preserve pending updates. Start only one candidate. Account for active history, pending controls, delayed provider retries and uncertain effects.

Rollback stops the candidate first and preserves its current state/evidence. Restart old code against the latest compatible database and same identity/volumes; do not restore a stale pre-cutover snapshot simply to revert code. If corruption makes that unsafe, keep both stopped, preserve snapshots and reconcile external deliveries before any restore. Never forget new receipts, automatically resend uncertain work, re-pair WhatsApp or provision replacement tokens as a rollback shortcut.

Release blockers are required-runtime incompatibility, unexplained divergence, unsafe rollback compatibility, or unresolved critical delivery/security defects. A promising canary or predominantly passing suite cannot override these blockers.

## Required implementation completion report

When later authorized work is complete, report changed behavior and architecture, full inventory coverage, baseline/candidate revisions and artifacts, test/probe/soak/canary outcomes, exception IDs and authority, schema/current-state rollback results, unresolved limitations and release decision. State clearly whether deployment has been authorized/performed. Keep secrets and private test identities out of the public report. Ask only for new ambiguous semantic decisions or concrete operational authorization that has not already been granted.
