# Tasks

Checked items record narrowly verified work from implementation checkpoint `checkpoint-clojure-evidence`, not completed release gates. Before checking another item, update the relevant tests, `docs/rewrite/coverage-matrix.md` and evidence ledger with its command, source hashes, result, skips and artifacts. The original 245-case inventory and document 02 remain exhaustive. All eight gates are still pending.

## 1. Baseline and independent oracle — Gate 1

- [x] 1.1 Freeze baseline `checkpoint-clojure-baseline`, tracked file/archive hashes and handoff inputs; verify `docs/rewrite/evidence/baseline.json` and `bun run check:rewrite:oracle` reconstruct/verify the original independently.
- [x] 1.2 Inventory all 245 original cases, including 11 opt-ins, and map significant workflows; verify `docs/rewrite/evidence/inventory.json` and `docs/rewrite/coverage-matrix.md` retain the original source locations and pending coverage.
- [x] 1.3 Run the original default suite with provider networking disabled; verify the recorded Linux result is 234 pass, 11 skip, zero failures.
- [x] 1.4 Establish independent deterministic rule/storage/adapter/effect/text replay; verify shared synthetic inputs produce strictly compared traces and per-transition state without sharing implementation logic.
- [x] 1.5 Record remaining transport/media/structured-content baseline scenarios with frozen time, bytes and scripted failure responses; verify independently reproducible traces and state without provider access.
- [x] 1.6 Record Managed Bots, picker/link/unmirror and command/control baseline transcripts; verify partial/stale/unauthorized cases and their inventory references before candidate comparison.
- [ ] 1.7 Record edit/reaction/revocation/poll baseline transcripts; verify message-copy scope, retries, retained controls and the known correctness candidates are characterized without preapproving fixes.
- [ ] 1.8 Record history baseline transcripts and actual legacy checkpoint shapes; verify ordering/cycles, choices, fresh copies, day markers and partial recovery deterministically.
- [ ] 1.9 Record Status and metadata baseline transcripts; verify refresh races, mutes, roster validity, permissions and quiet scheduling without live collection.
- [ ] 1.10 Record operational tooling, ignored updates and remaining legacy recovery inputs; verify the original can reproduce their observable outcomes from synthetic state.
- [ ] 1.11 Reconcile all significant workflow/failure paths with fixtures and inventory references; verify the complete secret-free corpus and reproducibility before accepting Gate 1.

## 2. Runtime and dependency compatibility — Gate 2

- [x] 2.1 Pin Bun 1.4.2 and nbb 1.6.214 with frozen npm resolution; verify package/lockfile pins and successful production dependency installation in the isolated Linux verification image.
- [x] 2.2 Verify basic namespace/npm resource loading, SQLite, AES-GCM, fetch, streams, multipart/openAsBlob, file APIs and explicit Bun child execution; retain `test/rewrite/runtime.test.ts` and current Linux probe evidence.
- [ ] 2.3 Exercise actual codec subprocess startup, resource resolution, Puppeteer/browser JS boundaries and descendant timeout/cleanup under the target Linux codec image; verify each required deployed path without Node/JVM fallback.
- [ ] 2.4 Compose the complete application entry and worker launch only after their required workflows exist; verify both the required `bunx --bun nbb src/main.cljs` command and packaged preinstalled startup with equivalent local/Compose configuration.
- [ ] 2.5 Implement and test signal handling, timer cancellation, paced scheduling and normal/forced draining; verify receipts and recoverable state after an interrupted long operation and update startup/shutdown documentation.
- [ ] 2.6 Accept Gate 2 only after every required deployed path and prolonged synthetic operation has executable evidence; report any unsolved required-runtime incompatibility as a release blocker.

## 3. Persistence, identity and pure rules — Gate 3

- [x] 3.1 Port the original storage operations, account/alias rules, cipher layout and deterministic formatting/configuration/cron rules; verify existing schema metadata, exact stored JSON, UTF-16 entities and the rule fixture corpus against the oracle.
- [x] 3.2 Verify synthetic old-write/new-read, new-write/old-read, core old/new continuation, encryption cross-decryption and tamper rejection; retain `test/rewrite/store.test.ts` evidence without printing credentials.
- [ ] 3.3 Add actual persisted checkpoint fixtures for all remaining history, metadata, control, retirement, binding and partial-media workflows; verify both implementations safely continue the latest candidate-written state.
- [ ] 3.4 If any additive state/schema extension becomes necessary, document its purpose and prove current-state old-code continuation in both directions; leave critical compatibility conflicts blocking Gate 3/release.
- [ ] 3.5 Complete pure rule/routing characterization for remaining inventory paths and exception decisions; verify strict unaffected comparisons and the full Gate 3 acceptance record.

## 4. Authenticated ingress and adapters — Gate 4

- [x] 4.1 Implement raw-body HTTP authentication, size caps, routes/status, owner admission, bot/topic scope, deduplication and explicit uncertain-send review; verify response/state parity in `test/rewrite/ingress.test.ts`.
- [x] 4.2 Implement Telegram/OpenWA JSON/multipart adapters and safe refusal/read/mutation classification; verify the scripted adapter matrix and actual synthetic loopback JSON/multipart probes against the oracle.
- [x] 4.3 Preserve absent/null and JavaScript coercion at signed/hashed/durable boundaries; verify signed age boundaries, malformed legacy-shaped IDs, literal stored bytes and no unexplained ingress-state delta.
- [ ] 4.4 Complete the real fake-provider HTTP fault matrix for definite refusal, 429 delays, connection refusal, read failure, timeout after modeled acceptance, invalid/missing results and multipart/entity bytes; verify safe errors and stable OpenWA idempotency headers.

## 5. Core text relay and worker — Gate 4

- [x] 5.1 Implement private/inbox/group text routes, new topics, readiness/membership selection and verified own-message attribution; verify ordered requests, copies and provenance with the independent relay fixtures.
- [x] 5.2 Preserve frozen targets/presentation/packet boundaries, preferred-bot fallback and pre-delivery-only topic repair; verify partial retry after route/binding changes and refusal after accepted/uncertain parts.
- [x] 5.3 Preserve quote conversation/thread/private-bot scope, deleted-copy exclusion and historical quote previews; verify each scope refusal and its durable state.
- [x] 5.4 Implement incoming/outgoing mention transforms with explicit roster checks and existing cache/freeze semantics; verify native/link rendering, invalid/departed notices and later text continuing without an unauthorized send.
- [x] 5.5 Implement the guarded paced worker, stable send keys, independent read jobs, retry exhaustion and legacy forwarding convergence; verify overlapping ticks, blocked/independent streams, missing IDs and done-receipt reuse.
- [ ] 5.6 Verify fully composed ingress/adapters/relay in an offline application fixture, including queued notices and cleanup interaction; retain end-to-end traces and complete Gate 4 only after the media tasks and fault matrix also pass.

## 6. Media and structured contents — Gate 4

- [ ] 6.1 Implement bounded authenticated WhatsApp/Telegram media retrieval and private file handling; verify streaming caps, realpath/symlink containment, endpoint allowlists and unavailable-media notices with source-copy parity.
- [ ] 6.2 Implement every inbound/outbound image, video, animation, video-note, audio, voice, sticker and document path; verify native requests, captions/entities, original-document fallback and archived-media reuse against the inventory.
- [ ] 6.3 Implement location, vCard/contact and poll content delivery; verify source formatting, payloads, quote behavior and mappings without adding unsupported update forwarding.
- [ ] 6.4 Port isolated sticker/conversion workers and justified browser JS resources; verify frozen byte/hash regeneration, alpha/timing/duration/size, process-group timeout and private intermediate cleanup.
- [ ] 6.5 Add partial-media/refusal/crash fixtures and operational codec notes with each media implementation; verify accepted label/attachment parts are never resent on independent read or cleanup failure.

## 7. Managed Bots and group transitions — Gate 5

- [ ] 7.1 Implement official Managed Bots create/event/token retrieval, refresh, encryption and local API migration/webhooks; verify provider-scripted event shapes, partial setup recovery and readiness preservation without manual token substitution.
- [ ] 7.2 Implement explicit binding and Start/readiness UX; verify multiple pending contacts, stale/conflicting binding, unbound DMs and ready/unready routing with literal buttons/text.
- [ ] 7.3 Implement durable inbox retirement independently of delivery; verify shared-topic/General guards, cleanup refusals, retries and lazy fallback restoration without automatic history import.
- [ ] 7.4 Implement scoped group deep links, request-chat picker and manual linking; verify owner/account/request/route/expiry/reuse checks, unique private destination and administrator requirements.
- [ ] 7.5 Implement unmirror with topic/effect receipts and route checkpoints; verify permissions, uncertain creation, mute/ignore retention, active-history guard and stale retry protection.
- [ ] 7.6 Update workflow coverage and operator guidance with these fixtures; retain real Managed Bots/picker permission/UI checks as externally uncertain until authorized canary evidence exists.

## 8. Commands, controls and cleanup — Gate 5

- [x] 8.1 Implement message observation, command parsing and core ledger tracking needed by ingress/text; verify cleanup observation does not authorize non-owner forwarding and preserves private/group/topic scope.
- [ ] 8.2 Implement every documented command, addressed-bot rule and menu install/startup path; verify exact usage/context replies, edited/former command suppression and service-message handling.
- [ ] 8.3 Implement the interactive mention picker and its token/pagination controls; verify roster refresh failure, collisions, alias changes, context/expiry checks and copied-tag behavior.
- [ ] 8.4 Implement notice/command/control TTL processing and clear log/all; verify cutoffs, protected/old messages, private bot scope, batch-to-single fallback and idempotent partial deletion independently of sends.
- [ ] 8.5 Characterize keep routing and generic callbacks using source, tests and invariants; create exception-register entries, preserve oracle behavior unless a justified correction is verified, and obtain focused decisions for ambiguous semantics.
- [ ] 8.6 Land control replay and operator documentation with each workflow; verify stale/repeated actions cannot affect another valid scope or undo a newer specialized-control decision.

## 9. Edits, reactions, revocations and poll actions — Gate 5

- [ ] 9.1 Implement incoming/outgoing edits, saved presentations, mention changes, echo suppression and refused-edit notices; verify clearing/late/empty edits and independent continuation after definite refusal.
- [ ] 9.2 Characterize split-message edits under the exception policy; verify baseline reproduction, any authorized exact delta and strict unaffected replay before recording a corrected result.
- [ ] 9.3 Implement actor/snapshot reaction reconciliation, native/fallback routes, custom emoji and owner echoes; verify copy selection, refusal-cache expiry and unresolvable-emoji notices without clearing prior source state.
- [ ] 9.4 Implement revocation tombstones and durable other/own-message Delete/Keep workflows; verify late-arrival/history suppression, expiry, partial cursor recovery and preserved mappings.
- [ ] 9.5 Implement poll vote controls and selection persistence; verify multi-choice toggling, authorization/retries and the existing unhandled `poll_answer` behavior, updating message-action coverage as it lands.

## 10. Conversation history — Gate 5

- [ ] 10.1 Implement history prompts, selection and cancellation; verify counts, complete context/binding validation, active-run exclusion and live delivery continuing before selection.
- [ ] 10.2 Implement complete batch fetch and frozen plan construction; verify deep-history options, wrong-chat/revoked/Status filtering, deduplication, parent ordering, ties and cycle guards.
- [ ] 10.3 Implement destination/run-scoped copies, day markers, skip/all behavior and quote/media reuse; verify partial retries, cleared archives, saved timezones/styles and actual legacy checkpoints.
- [ ] 10.4 Implement completion/failure and scoped live holds; verify other streams continue, failure releases the hold and the final count retains its documented meaning, updating history replay/docs.

## 11. Status archive — Gate 5

- [ ] 11.1 Implement account-scoped topic/startup/subscription refresh and valid Status normalization; verify expired/malformed rejection, deduplicated races, supported media and unavailable captions.
- [ ] 11.2 Implement canonical author mutes, pagination and revision-safe controls; verify suppression before downloads, stale retries and alias/reopen behavior without changing conversation routes.
- [ ] 11.3 Implement read-only interaction responses and topic cleanup; verify no outgoing WhatsApp write, retained archive after source expiry, scoped clearing and unmute without replay, updating Status fixtures/docs.

## 12. Metadata and scheduling — Gate 5

- [ ] 12.1 Implement coalesced scope/run/target/waiter orchestration and warnings; verify partial target failures, chat priority and manual/bind/link/new-request completion.
- [ ] 12.2 Implement complete roster and permission validation plus contact/topic/mirror metadata; verify shared management identity, missing-bot guidance, cached-data preservation and no automatic membership changes.
- [ ] 12.3 Implement allowed photo retrieval/content hashes and persisted quiet cron; verify rotated URLs, absent photos, timezone/minute restart behavior and unchanged quiet results, updating metadata replay/docs.

## 13. Complete replay, exceptions and recovery — Gates 1, 3, 5, 6

- [x] 13.1 Verify core text SIGKILL recovery at seven boundaries in both directions through both readers; retain 14 scenarios, 28 crashed writer processes and 56 consistent latest-state continuations in `test/rewrite/crash.test.ts`.
- [ ] 13.2 Expand replay to every original inventory case and failure/recovery shape; compare per-transition state, requests, text/entities/buttons, checkpoints and selected jobs with zero unexplained divergence.
- [ ] 13.3 Expand process-crash and graceful/forced shutdown injection through media, mappings, history items/days and control/binding/route notifications; verify frozen retry, uncertainty and independent cleanup/read recovery.
- [ ] 13.4 Verify all actual legacy/current workflow shapes with old-write/new-read, new-write/old-read and old-code processing of latest candidate state; use private consistent clones only when separately authorized.
- [ ] 13.5 Complete the exception register with exact deltas, authority, regression/replay and current-state rollback evidence; defer unrelated noncritical defects and leave critical or ambiguous unresolved conflicts blocking release.
- [ ] 13.6 Reconcile the full inventory and gate ledger after these checks; accept Gates 1, 3, 5 and 6 individually only when each criterion is met, without treating component or historical passes as full acceptance.

## 14. Operational tooling and packaged application

- [ ] 14.1 Port/retain configure, setup, callback allowlisting, identity, sync, controls, review and transfer tooling; verify legacy inputs, literal private configuration, safe startup boundaries and documented recovery commands.
- [ ] 14.2 Replace remaining unfinished workflow hooks with the verified implementations and compose the application lifecycle; verify the complete default suite and an offline startup that cannot contact real providers.
- [ ] 14.3 Prepare frozen dependency/system-codec packaging and local/Compose wrappers; verify environment equivalence, private/shared volume permissions, worker resource paths and a single application writer in the actual target image.

## 15. Linux operations, soak and rollback rehearsal — Gate 7

- [ ] 15.1 Run complete default, real codec/sticker and Docker transfer/restore suites in the target Linux environment; retain artifact/version hashes and resolve required skips or failures before accepting the capability.
- [ ] 15.2 Rehearse candidate writes then the matching old image on the latest synthetic database and same synthetic volumes; verify identity, credentials, pending jobs, receipts and media continuity without stale restoration.
- [ ] 15.3 Prepare the sustained synthetic workload and accelerated cleanup/cron/expiry/backlog boundaries; verify telemetry captures workload, elapsed time, RSS, event-loop/latency and queue behavior without a fabricated target.
- [ ] 15.4 Run the planned real 24-hour elapsed synthetic observation and boundary checks; retain measurements and resolve unexplained unbounded growth or backlog recovery failures.
- [ ] 15.5 After separately authorized read-only deployment inspection, record actual deployment revision/configuration and deployed-only differences; verify no private content enters public artifacts and complete Gate 7 evidence.

## 16. Authorized external verification and release — Gate 8

- [ ] 16.1 Prepare a concrete canary/capture plan with candidate/image hashes, privately held dedicated identities, filters, checklist, scope/duration/operator, stop conditions and rollback commands; verify it is reviewable without executing live setup or sends.
- [ ] 16.2 Obtain explicit capture/canary execution authorization for that plan; record its scope before collecting any real event, changing a webhook, provisioning a bot or starting a live candidate.
- [ ] 16.3 Execute only the authorized dedicated single-writer capture/canary; verify the complete document 08 checklist with expected/actual evidence and no unrelated collection, leaked secret or automatic uncertain retry.
- [ ] 16.4 Resolve required canary failures and critical exceptions; retain monitored harmless failures as failures rather than passes and accept Gate 8 only when its required criteria are satisfied.
- [ ] 16.5 Deliver the implementation completion/cutover report with all gates, coverage, revisions, exceptions, compatibility/soak/canary results, deployment differences and latest-state backup/rollback sequence; obtain separate production execution approval.
- [ ] 16.6 Execute production cutover only within that approval, stopping the old writer first; verify pending updates, controls/history/retries, identity, receipts and rollback continuity, and record whether deployment actually occurred.

## Workflow follow-up

- Archive this OpenSpec change only after its tracked implementation and acceptance work is complete and the owner review requirements are satisfied; do not archive it to make an incomplete rewrite appear finished.
- Verify archived main specifications and links after the archive workflow. Planning readiness alone grants no capture, canary or production-release authorization.
