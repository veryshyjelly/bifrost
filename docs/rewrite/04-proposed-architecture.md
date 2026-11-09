# Proposed ClojureScript architecture

Decision update (2026-11-10): architecture is finalized. [08 Implementation specification](08-implementation-specification.md) governs future authorized work, including correctness exceptions and backward-compatible schema extensions. Earlier strict-parity/pending-review wording below is historical where it conflicts with that contract. Implementation and deployment remain unauthorized.

**Recommendation, pending review:** one nbb process on Bun, the same SQLite schema/JSON/effect keys and existing external services. Plain maps and functions express decisions; JavaScript interoperability is concentrated at I/O boundaries. No framework, ORM, message broker, extra service, Telegram SDK or OpenWA npm client is justified.

## Namespaces and concrete responsibilities

| Namespace | Responsibility and existing justification |
|---|---|
| main / bifrost.lifecycle | Validate environment; construct dependencies; HTTP, worker and timers; drain on signals (`server.ts:63`) |
| bifrost.config / identity | Exact defaults, validation, crypto primitives, stable account (`config.ts:12`, `identity.ts:7`) |
| bifrost.http | Raw-body authentication, size limits, public/admin routes, admission (`server.ts:8`) |
| bifrost.queue / effects | Existing SQL selection/order/backoff and receipt transitions (`store.ts:54`, `bridge.ts:64`) |
| bifrost.store | Synchronous SQLite and sealed credential persistence; preserve SQL transactions (`store.ts:10`) |
| bifrost.telegram / openwa | fetch/JSON/multipart, safe errors, timeouts, exact provider methods (`api.ts:18`, `openwa.ts:6`) |
| bifrost.routing | Route/context/target selection and fallback preconditions (`bridge.ts:47`, `146`, `285`) |
| bifrost.relay | Ordered workflow orchestration, mapped receipts and completion (`bridge.ts:294`, `516`) |
| bifrost.registration | Managed Bot event, token refresh, explicit binding, readiness (`bridge.ts:781`) |
| bifrost.groups | Picker validation, link/unmirror and inbox retirement (`bridge.ts:220`, `806`, `880`) |
| bifrost.commands / controls | Dispatch and existing contextual commands; durable notice/delete ledger (`control.ts:17`) |
| bifrost.format / mentions | Pure UTF-16 entity transforms plus explicit roster/token operations (`format.ts:1`, `mentions.ts:19`) |
| bifrost.media / sticker-worker | File caps/path safety, conversion and isolated expensive rendering (`media.ts:13`, `stickers.ts:13`) |
| bifrost.history / statuses / metadata | Existing independent durable run/checkpoint workflows |
| bifrost.reactions / revocations | Actor/copy-specific reconciliation and own-message archive choice |

These names are a dependency map, not a requirement to create every namespace immediately. Keep small related functions together until extraction reduces coupling. Avoid replicating Bridge and helper classes or implementing a generic service/protocol hierarchy.

## Data and effects

Pure functions receive only the relevant snapshot, event, clock value and resolved IDs; return a decision map with explicit next operations. Formatting, canonicalization rules, command parsing, history ordering, desired reaction calculation, callback validation and route transition rules are strong pure boundaries. Database queries and API-dependent membership checks remain explicit orchestration; do not pretend routing is pure while secretly calling providers.

Use immutable maps for domain values. A small relay map may hold source account/chat/message, author, own flag, kind, text/entities, quote and media descriptor because these concepts recur in bridge/history/status. Preserve original provider payload alongside it for unsupported types and replay. Do not impose a universal messaging schema that erases Telegram bot/thread scope, Status expiry, mention metadata or delivery provenance.

Pass a dependency map of store functions, provider functions, clock, UUID/random supplier, file/codec operations and scheduler into workflow functions. Credentials belong in adapter closures. Tests substitute functions; no dependency injection container or custom protocols needed. Explicit durable transactions use synchronous BEGIN/COMMIT/ROLLBACK around existing atomic groups; never hold a SQLite transaction across a Promise.

Return JavaScript Promises from every asynchronous workflow and await/chain all mutations in existing order. Use nbb's bundled promesa.core/let when it makes sequencing readable; it introduces no extra installed dependency. Keep one busy atom and one paced worker. Preserve fair-enough current queue priority before considering performance changes; an interpreter is likely I/O-bound here, but there is no benchmark to support performance claims.

Do not use core.async. Disposable nbb 1.6.214 could not resolve cljs.core.async. Channels/go would require additional compatibility work, while durable queue ordering and JS Promises already solve the actual coordination problem. Do not substitute Babashka or introduce its dependency resolver.

## Runtime evidence and limitations

Reproducible offline probe: [experiments/runtime-probe.cljs](experiments/runtime-probe.cljs), run with `bunx --bun nbb docs/rewrite/experiments/runtime-probe.cljs`. It uses only synthetic data and memory SQLite.

Disposable experiment directory: `/tmp/bifrost-nbb-probe`; no production edits. Exact launcher tested from that directory: `bunx --bun nbb src/main.cljs`. Available nbb reported 1.6.214, Bun 1.4.2 macOS arm64. The script asserts js/Bun.version, so successful invocation is actually Bun, not a Node shebang fallback.

| Capability | Evidence/status |
|---|---|
| Namespace imports of node:sqlite, crypto, http, fs, child_process | Passed under nbb/Bun |
| DatabaseSync constructor, prepare/get and DDL | Passed using memory SQLite |
| SHA256 and Promise completion | Passed |
| Bun subprocess execution | Passed; child reported 1.4.2 |
| AES-GCM round trip / openAsBlob + FormData | Passed in disposable extension |
| core.async | cljs.core.async namespace unavailable in tested installation |
| Existing JSON/multipart HTTP behavior | Existing TypeScript HTTP suite passed outside sandbox; nbb adapter transport still requires probe |
| Streams, AbortSignal timeout, redirects, realpath containment, Unix process groups | Existing implementation evidence; complete nbb-path contract testing pending |
| Puppeteer dynamic import, lottie resolution, rendering/alpha/FFmpeg/Pillow | Production code exists; full nbb/Linux codec execution unverified |
| Signal draining, prolonged polling/timers, memory/event-loop behavior | Unverified under nbb; must soak-test target image |

The initial probe used js/typeof as though it were a callable function and failed; corrected to fn?. A later probe extension had an unmatched parenthesis, corrected before recording pass. These were experiment mistakes, not runtime incompatibilities. Initial HTTP loopback failures vanished when permitted outside sandbox.

The official [nbb README](https://github.com/babashka/nbb/blob/main/README.md) describes SCI subset evaluation, dynamic npm imports and bundled Promesa. Recommendation: use maps/functions and `$default` where required, avoid deftype/compiler-dependent libraries, and probe every required namespace. This documentation is guidance; local probes and pinned-image tests decide compatibility.

Package resolution is relative to scripts; child worker must have an explicit nbb/Bun launcher and classpath. Existing sticker launcher invokes process.execPath with a .ts entry point, and worker uses import.meta.url/resolve: these cannot simply be copied to interpreted .cljs. Use nbb.core/*file* and explicit known resource paths; launch worker with Bun and pinned nbb entry, assert Bun there too. Keep process group timeout and global serialization. Pure ClojureScript cannot replace Puppeteer's page JS execution by passing an interpreted function; provide a minimal static browser JS resource or string at that boundary. Validate it against original rendering fixtures.

Recommend add only pinned nbb npm package, retain lottie-web/puppeteer-core and existing system codec dependencies. Pin candidate 1.6.214 only after target-image tests; current production lockfiles remain unchanged. Avoid Maven deps requiring bb/JVM. Keep explicit Bun guard; preinstall dependencies with frozen lockfile so bunx does not download at service startup. Bare launcher does not promise bridge.env loading: preserve existing explicit env-file wrapper for local operations and Compose env injection.

No claim that all required behavior is validated yet. Required runtime acceptance gates are executable ingress/transport/stream/crypto/store equivalence; npm imports and codec matrix inside the pinned Linux image; kill/restart/drain/soak; memory and backlog recovery measurement. Runtime decisions are fixed, but failures must be solved within nbb/Bun, not by silently switching execution.
