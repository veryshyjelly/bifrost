# Implementation evidence ledger

Offline implementation authorized by the owner on 2026-11-10. Live capture, canary execution, cutover and deployment are not authorized or performed.

Baseline is `checkpoint-clojure-baseline`; candidate progress is committed on `clojure/rewrite`. Check reports label the tested tree as uncommitted because validation preceded the progress commit; their per-file hashes identify the exact tested source independently of commit metadata. [baseline.json](evidence/baseline.json) freezes all 71 tracked files, archive digest and initially available untracked handoff inputs before candidate edits. Documents 01–07 were restored after the initial capture; their hashes are frozen with document 08 in [inventory.json](evidence/inventory.json). No tracked changes existed at freeze. Original sources/tests remain intact and independently runnable from the verified oracle. Deployment-only differences were not inspected.

`bun run check:rewrite:oracle` verifies every frozen file and reconstructs the oracle from the recorded Git revision if needed. It refuses to use modified oracle sources. Private working files are never copied into it. `bun run prepare:rewrite` builds an allowlisted public-source Docker context with the frozen oracle and a hashed candidate input manifest. Its image is a verification image, not the production codec/service image.

| Gate | Status | Current evidence / outstanding requirements |
|---|---|---|
| 1 Freeze baseline/oracle | pending | Source and dependency/image pins frozen; all 245 original tests inventoried. Original defaults pass locally and in network-disabled Linux. Pure/store/adapter/effect deterministic comparisons exist. Full workflow fixture corpus and recovery inventory replay remain pending. |
| 2 Runtime compatibility | pending | nbb 1.6.214 and Bun 1.4.2 pinned; current probe results recorded below. Exact application entry, lifecycle/draining/timers, codec execution/browser JS boundaries, process groups and prolonged operation remain pending. |
| 3 Persistence/pure rules | pending | Storage schema/JSON, queue, mappings, account, aliases, encryption, synthetic cross-version writes/recovery/continuation and pure-rule comparisons implemented. Full workflow checkpoint/current-state old-image rollback still pending; no schema extensions. |
| 4 Transport/core relay | pending | Adapters, authenticated HTTP/admission, core text relay, frozen targets/packets, scoped quotes, mentions, fallback/topic repair and paced worker compared against oracle. Full transport fault matrix, media and integrated application lifecycle remain pending. |
| 5 Full replay | pending | [Coverage matrix](coverage-matrix.md) and per-test inventory exist. Broad workflows and separately authorized real recordings remain pending. |
| 6 Failure/recovery | pending | Text relay now has actual SIGKILL tests at seven durable/provider boundaries in both directions and all old/new writer/reader combinations, alongside queue/effect comparisons. Broader persisted workflows, graceful shutdown and exhaustive release recovery remain pending. |
| 7 Codecs/deployment/rollback | pending | No candidate codec, Docker-transfer, 24-hour soak or latest-state old-image rollback claim. Original opt-in checks remain skipped. |
| 8 Controlled canary | pending | No live execution. Prepare concrete plan and obtain separate execution authorization after offline requirements. |

## Commands and evidence

All checks use synthetic data. Local runtime is macOS arm64; isolated Linux checks use aarch64/arm64. Bun reports `1.4.2 (744846f84)`; nbb reports `1.6.214`; exact npm resolution/integrities are in candidate `bun.lock`. The target base is `oven/bun:1.4.2-debian@sha256:4f6e31d1a54d6a3dd312daef655fc998101b5043d52e12592ac293ef04b9bc73`. Provider pins remain unchanged in the frozen `deployment.lock.json`.

Initial checks on 2026-11-10:

- `bun run test` before edits: sandbox restricted loopback; 229 pass, 11 skip, 5 fail. Repeated with loopback permitted: **234 pass, 11 skip, 0 fail**, 245 original cases. These were environment failures, not baseline defects.
- Frozen original `bun test ./test` in a disposable Linux/aarch64 container, `--network none --read-only`, tmpfs `/tmp`, oracle mounted read-only at `/oracle`, workdir `/oracle`, no production volumes or host ports: **234 pass, 11 skip, 0 fail**. Used cached local verification image `local/bifrost:0.1.0` (`f65af24b14d5`); this is not evidence about a running deployment. Repeating with the pinned candidate verification image is recorded separately.
- `bunx --bun nbb docs/rewrite/experiments/runtime-probe.cljs`: original synthetic probe passed locally. Historical probes are not substituted for target Linux evidence.
- `bun test ./test/rewrite`: candidate rule/store/adapter/effect probes compare independent child processes against the frozen source, with synthetic clocks/credentials and no provider calls. Runtime probe binds only loopback. Exact final repeated check results, candidate input hashes, timestamps and log artifact hashes are in `evidence/checks/`.

Recorded foundation checks on 2026-11-10 (245 candidate passes, before ingress/relay implementation):

| Check | Result | Evidence |
|---|---|---|
| Entire candidate/local default suite | Latest repeated source snapshot/counts in linked report | [local.json](evidence/checks/local.json) |
| Entire candidate/Linux default suite, network disabled | Latest repeated source snapshot/counts in linked report | [linux.json](evidence/checks/linux.json) |
| Frozen original/Linux default suite in same verification image | 234 pass, 11 skip, 0 fail; 245 cases | [baseline-linux.json](evidence/checks/baseline-linux.json) |
| Existing repository audit | Pass, 71 tracked files; does not audit untracked additions | [privacy.json](evidence/checks/privacy.json) |

An additional credential/private-artifact pattern scan covered 115 tracked and untracked public files, passing without printing credential values. This pattern scan is not provider or production-state verification. All 11 opt-in skips remain explicit outstanding work.

To reproduce the current checks, install with `bun install --frozen-lockfile`, run `bun run check:rewrite:oracle`, then `bun run test:rewrite` or `bun scripts/rewrite/check.ts local` (loopback must be permitted). For Linux:

```sh
bun run prepare:rewrite
docker build -f docs/rewrite/experiments/Dockerfile -t local/bifrost-rewrite-checks:foundation data/rewrite/context
bun scripts/rewrite/check.ts baseline-linux
bun scripts/rewrite/check.ts linux
```

The check script refuses a stale image by comparing every source/dependency hash against the manifest embedded at build time. It executes the inspected immutable image ID and records that ID, candidate fingerprint, exact command, UTC timestamps and private log hash. Build dependencies are preinstalled from the frozen lockfile; execution uses `bunx --bun --no-install nbb` and never installs packages. No service startup command is switched to ClojureScript yet: `src/main.cljs` awaits the actual relay/lifecycle implementation.

## Boundary findings and remaining uncertainty

The comparison harness initially converted JS `undefined` through `clj->js`, producing `null`. The harness now constructs JS outcomes directly; absent fields, explicit nulls and stored JSON bytes compare strictly. No normalization erases those distinctions.

SCI wraps errors thrown from interpreted code with source context. Adapter classification explicitly unwraps that context and recovers original API error data through causes. The scripted matrix verifies connection-refusal classification, mutation ambiguity and sanitized reasons across Promise boundaries. Raw unexpected errors remain conservative.

`nbb.core/*file*` is dynamically bound while loading; async resource operations must capture its value at namespace load. The current probe does this. It exercises npm resource resolution/imports; it does not launch Chromium or establish codec/browser parity.

Current checks establish implemented and locally verified behavior only. They do not establish external provider conformance, full rewrite parity, latest-state production rollback or release readiness. No correctness candidate has been evaluated and no intentional observable exception is registered yet.

## Authenticated ingress and core text relay

The next slice adds functional routing, message observation, authenticated HTTP ingress/review, text delivery/copy provenance, mention transforms with explicit membership/roster I/O, and a single guarded worker. Its dependencies remain explicit maps; storage uses the original tables/keys and JS boundary objects. The TypeScript runtime and original tests remain untouched. Recognized unfinished workflows reject explicitly and cannot be silently marked successful; no candidate production entry point is exposed yet.

`ingress.test.ts` compares HTTP responses and all six durable tables after each request/mutation. `relay.test.ts` independently compares ordered synthetic provider requests and table contents after each transition, including frozen retries, legacy presentation, mapping scopes, missing IDs, independent read jobs, eight-attempt exhaustion and mention notices. The invalid-tag regression exposed a candidate mismatch: early validation rejection bypassed the intended scoped notice. Validation now resolves a notice/outgoing decision before subsequent I/O, and the independent oracle regression verifies the original behavior. This is candidate parity repair, not a baseline correctness exception.

Ingress fixtures include valid signatures at both seven-day boundaries and just outside them, plus absent/null management IDs and legacy-shaped Telegram chat data. Durable-key construction retains JavaScript string coercion (`undefined` versus `null`) rather than allowing Clojure string conversion to collapse either into an empty string. Independent fixture HTTP connections avoid coupling authentication/body-cap assertions to client connection-pool timing; the product's HTTP handler is not configured to disable keepalive.

`crash.test.ts` sends SIGKILL to actual original/candidate subprocesses at seven points in both directions: enqueue, claim, before modeled remote acceptance, after acceptance, receipt persistence, mapping and before job completion. It compares crashed state, then resumes consistent clones through both old and new implementations. The synthetic provider journal remains outside Git, records acceptance before returning, and verifies that recovery does not repeat the send. Interrupted sending effects become uncertain; done receipts are reused. These tests model remote acceptance; they are not provider verification or graceful shutdown tests.

The crash harness uses `VACUUM INTO` for consistent synthetic SQLite copies. A bare database-file copy after a crash lost WAL contents in the initial harness; no product state or private database was involved. Do not infer that closing an inspection connection alone safely checkpoints every crashed database.

Native Clojure MCP evaluation is now available and verified against Bun 1.4.2, with namespace reloads and in-memory synthetic checks. No custom MCP transport wrapper remains. Setup/editor artifacts such as `.nrepl-port`, `.calva/`, `.clj-kondo/` and `.lsp/` are owner work, are not build-context inputs, and are not added to the baseline or public fixtures.

Latest local/Linux full-suite checks are recorded in `evidence/checks/` with exact source fingerprints. All eight release gates remain pending; this slice does not complete media, commands/registration/groups/history/Status/metadata, codecs, lifecycle, live canary or deployment validation.

Recorded completion of this slice: **278 pass, 11 skip, 0 fail** on both macOS/Bun and isolated Linux/Bun (289 cases across 25 files). The frozen original independently reports **234 pass, 11 skip, 0 fail** in the same Linux image. The 14 SIGKILL scenarios exercise 28 crashed writer processes and 56 old/new recovery continuations. Logs and immutable image/source identifiers are in the linked check reports; the 11 original codec/transfer skips remain outstanding.

## Media/structured transport recordings

[Task 1.5 evidence](media-baseline.md) adds 73 original-only media/structured scenarios and 27 transport failure scenarios, reproduced twice in independent subprocesses. The checked-in manifest fingerprints exact ordered traces, returned metadata and per-transition durable state; complete synthetic transcripts are regenerated in ignored `data/rewrite/evidence/media-baseline.json`. Real download/stream limits and shared-file containment execute original code; codec outputs remain scripted. Candidate media, codec validation and all eight acceptance gates remain pending.

The full suite now reports **279 pass, 11 skip, 0 fail** (290 cases across 26 files) on macOS/Bun and isolated Linux/Bun with networking disabled. The initial sandboxed local attempt could not bind its nine existing loopback tests; the loopback-enabled rerun and Linux run passed without source changes for that environmental restriction. Current source fingerprints and Linux image identity are in `evidence/checks/local.json` and `evidence/checks/linux.json`.

## Registration and control recordings

[Task 1.6 evidence](control-baseline.md) adds 47 original-only cases with 588 observed transitions: 38 unchanged original assertions with immutable inventory references, plus nine Managed Bot scenarios. Process-local observation records original workflow boundaries, direct SQL changes, ordered provider calls/results, errors and complete durable tables. Independent replays must reproduce full transcripts and their committed manifest. Fixed entropy is confined to test subprocesses; provider networking is forbidden. Original partial-setup prompt omission is characterized without changing it. Candidate comparison and all eight acceptance gates remain pending.

Latest full-suite verification: **280 pass, 11 skip, 0 fail** (291 cases across 27 files), both macOS/Bun and network-disabled read-only Linux/Bun. The original default suite and its 11 opt-ins remain unchanged. Current fingerprints and image identity are recorded in the local/Linux check reports.

## Edit, reaction, revocation and poll recordings

[Task 1.7 evidence](action-baseline.md) adds 54 original-only cases with 855 observed transitions, including 48 unchanged original assertions. Independent replays retain exact API requests/results, state, copy scope, retries, ambiguity and retained controls. Three known correctness candidates now have explicit original characterizations in the [register](exception-register.md), with no selected correction or approved delta. Poll selections and the admitted `poll_answer` no-op are recorded without inventing forwarding. The shared replay helper reproduces the earlier control transcript unchanged. Candidate comparison and all eight acceptance gates remain pending.

Latest full-suite verification: **281 pass, 11 skip, 0 fail** (292 cases across 28 files) on both macOS/Bun and isolated Linux/Bun. Original fixture source hashes, candidate input fingerprints and Linux image identity remain recorded and auditable.

## History recordings

[Task 1.8 evidence](history-baseline.md) adds 37 original-only cases with 571 observed transitions: all 35 original history assertions plus actual original processing of cyclic/self-referenced reply plans and a legacy job without a run ID. Raw legacy checkpoint fields, frozen batches, choices, fresh-copy scope, date markers and partial/worker recovery remain observable in exact transcripts. Empty drain ticks are omitted only with no errors/API calls and byte-identical state; earlier control/action transcripts remain unchanged. Candidate history comparison, exhaustive history crashes and all eight gates remain pending.

Latest full-suite verification: **282 pass, 11 skip, 0 fail** (293 cases across 29 files) on macOS/Bun and isolated Linux/Bun, with matching exact original transcript manifests. Source/image fingerprints remain in the check reports.

## Status and metadata recordings

[Task 1.9 evidence](status-metadata-baseline.md) adds 33 original-only cases with 414 observed transitions: all 32 original Status/metadata assertions plus a quiet populated-group scheduled run. Exact observations retain refresh races, persistent mutes and scopes, roster/permission safeguards, photo bytes/hashes, coalesced notifications and cron deduplication. Argument-free Date construction shares the fixed clock; only the photo fixture's random temporary parent is aliased. No live provider access or application behavior changes are involved. Candidate replay and all eight release gates remain pending.

Latest full-suite verification: **283 pass, 11 skip, 0 fail** (294 cases across 30 files) on both macOS/Bun and network-disabled read-only Linux/Bun. Earlier baseline manifests remain unchanged; current source and image fingerprints are retained in the check reports.
