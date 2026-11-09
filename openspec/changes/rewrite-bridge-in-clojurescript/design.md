# Design

## Context

See [proposal.md](proposal.md) for motivation and capability boundaries. The governing implementation contract is `docs/rewrite/08-implementation-specification.md`; its finalized decisions supersede conflicting historical recommendations in documents 01–07. The project already authorized offline implementation. This OpenSpec setup organizes that work without reopening architecture or expanding operational authorization.

The frozen oracle is `checkpoint-clojure-baseline`. Implementation checkpoint `checkpoint-clojure-evidence` contains storage, deterministic transforms, adapters, authenticated ingress, core text relay, mention transforms, paced work and modeled crash recovery. Current source hashes match recorded local/Linux checks: 278 passes, 11 opt-in skips; the original independently has 234 passes and 11 skips. These counts do not establish full workflow parity. All eight release gates remain pending.

## Goals / Non-Goals

**Goals:**

- Finish each inventory workflow against independent oracle evidence while preserving current-state recovery in both implementations.
- Keep decisions/presentation separate from provider and filesystem operations, retaining existing ordering and durable effects.
- Make OpenSpec tasks and the existing evidence ledger distinguish component completion from gate acceptance.

**Non-Goals:**

- Replacing the external services, data model, idempotency scheme or single-worker scheduling contract.
- Converting every TypeScript class into another object hierarchy, adding a framework/broker/ORM, or introducing unsupported product behavior.
- Migrating private data, provisioning live accounts, capturing events or deploying as part of planning or tests.

## Decisions

### Runtime and deployment boundary remain fixed

Use interpreted ClojureScript through pinned nbb on pinned Bun, including application workers, and preserve the external OpenWA and local Telegram API contracts. Verify the required bare entry command and a preinstalled/no-install production launcher with equivalent environment configuration. Preserve system codecs and browser dependencies. Node/JVM fallback, another runtime and a compiled replacement are excluded by the finalized contract, not pending alternatives.

### Functions and explicit dependencies retain durable orchestration

Use immutable decision maps and functions with dependency maps for clock, identity, storage, providers, files and scheduling. Use JavaScript Promises or bundled Promesa for ordered I/O, one paced worker and process-local busy/timer state. Synchronous SQLite transactions never span asynchronous operations. Brokers, parallel delivery workers, channel-based redesign and class translation are excluded because they would alter the established orchestration/recovery contract.

### Storage and wire representations stay compatible

Retain the original schema, keys, receipts and cipher layout; justify and test any additive extension against actual old-code continuation. Keep JS objects at durable/wire boundaries, including omission/null, numeric conversion and byte-sensitive JSON/hash semantics. Retain immutable formatting rules with UTF-16 offsets. A fresh schema or blanket object conversion would risk losing receipts and rollback compatibility and is not an accepted alternative.

### Independent replay establishes parity progressively

Keep the archived/hash-verified original runnable independently of the candidate. Shared fixture clients and scripted provider responses describe external inputs; they do not share routing/delivery implementations. Compare ordered external requests and durable state after transitions, reopening and crashes. Normalize only permitted nondeterministic representation. Self-comparison or merely updating goldens cannot prove parity.

The current text harness kills real subprocesses at seven boundaries and resumes consistent SQLite clones through both implementations. Its provider journal models remote acceptance; it does not prove provider behavior or power-loss durability. Use consistent SQLite/WAL handling rather than a bare database-file copy. Extend the same discipline to partial media and actual legacy workflow checkpoints.

### Existing progress is evidence, not a substitute for missing workflows

Carry the verified checkpoint forward. Finish media/structured content, edits/actions, Managed Bots, group transitions, command/control workflows, history, Status, metadata and tooling before exposing a candidate application entry. Recognized unfinished workflows currently reject explicitly; they are not successful no-ops or a deployable application. Preserve original tests and expand candidate replay using document 02 and the 245-case machine-readable inventory.

### Corrections use the finalized exception policy

Investigate keep routing, split-message edits and generic callbacks without preapproving changes. A demonstrated critical/migration-relevant violation can be corrected under Q11 with the full exception evidence; unrelated noncritical issues are deferred. Competing plausible semantics or unsafe rollback require a focused owner decision. Exact expected deltas and strict unaffected comparisons replace blanket quirk preservation or silent cleanup.

## Risks / Trade-offs

- **False confidence from a passing subset** → Keep workflow/task coverage explicit and all incomplete gates open; separate synthetic/local evidence from provider verification.
- **SCI/Promise and JS boundary differences** → Use native REPL checks plus fresh-process pinned-runtime comparisons, safe cause-aware errors and exact durable/wire comparisons.
- **Interpreter/browser boundary incompatibility** → Exercise imports, resource paths, codecs, child groups and teardown in the actual Linux codec image; use small browser JS resources only with fixture parity.
- **Partial sends and latest-state incompatibility** → Persist effect knowledge before external work, preserve uncertainty and verify candidate-write/old-code continuation on synthetic current state and separately authorized private clones.
- **Soak/resource uncertainty** → Measure real elapsed synthetic workloads, resource growth, scheduling/cleanup boundaries and backlog recovery; do not invent performance thresholds or replace elapsed observation with acceleration.
- **Live integrity/security risk** → Prepare a dedicated filtered single-writer canary plan and stop/reconcile affected activity on credible risk; monitoring does not waive delivery gates.

## Migration Plan

| Gate | Exit evidence still required |
|---|---|
| 1 Baseline/oracle | Complete deterministic corpus and significant workflow/failure coverage, retaining the frozen source, original tests and secret-free artifacts |
| 2 Runtime | Every required target Linux path, actual main/worker launch, signals/draining, codecs/browser resources and prolonged operation |
| 3 Persistence/pure rules | Complete legacy/current checkpoint compatibility and safe latest-state old-code continuation beyond the verified core subset |
| 4 Core transport/relay | Full real fake-provider fault matrix, integrated routing and all media/structured paths |
| 5 Behavioral replay | Every inventory workflow, partial/stale controls and zero unexplained divergence; separately authorized filtered recordings where needed |
| 6 Failure/recovery | Exhaustive relevant process/workflow crash windows, graceful/forced shutdown and old-code continuation of current candidate-written state |
| 7 Operations/rollback | Default and opt-in Linux checks, volume/transfer/codec validation, real 24-hour synthetic observation and latest-state old-image rehearsal |
| 8 Canary | Reviewed plan, explicit execution authorization, dedicated conversations, single writer and the complete expected/actual acceptance checklist |

After the gates, prepare the release report with actual read-only deployment differences, registered corrections, consistent latest backup, maintenance commands and demonstrated rollback. Canary completion does not approve cutover. An authorized cutover stops the old writer before starting one candidate. Rollback stops the candidate first and preserves its latest compatible state/volumes; corruption requires both stopped and delivery reconciliation before restoration.

Dedicated test identities, operators, capture filters and execution windows are later operational prerequisites, not unresolved architecture decisions. Commit validated offline milestones on `clojure/rewrite`, retaining source-hashed evidence and owner editor/runtime artifacts.
