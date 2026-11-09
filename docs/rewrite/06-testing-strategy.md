# Behavioral equivalence strategy

Decision update (2026-11-10): architecture is finalized. [08 Implementation specification](08-implementation-specification.md) governs future authorized work, including correctness exceptions and backward-compatible schema extensions. Earlier strict-parity/pending-review wording below is historical where it conflicts with that contract. Implementation and deployment remain unauthorized.

The existing test suite is the first oracle, not a complete provider conformance suite. Document 02 indexes observable cases. Baseline investigation: 229 pass/11 skip/5 sandbox HTTP failures; independent unrestricted HTTP rerun 6 pass/0 fail. Codec and Docker opt-ins remain required gates, not implied passes. Historical validation claims in VALIDATION.md are D for this investigation unless independently repeated.

## Replay format and comparison

Recommend JSON fixtures containing scenario ID, synthetic initial SQL/KV state, ordered ingress events with raw request bytes where authentication matters, clock/random draws, expected API requests and scripted results/errors, worker tick schedule, optional crash point, and expected resulting database/files/user output. Treat each scenario as a complete workflow, not isolated message snapshots. Keep real recordings outside Git until sanitized; include text/entities, quote IDs, LID/phone aliases, timestamps, expiry, reactions and media descriptors needed for routing. Use generated synthetic tokens and keys everywhere. Never serialize real auth headers, URLs containing tokens, session blobs, private content or unredacted SQLite files into design artifacts.

Run original and candidate independently from identical initial state and API script. Disable network and inject fake APIs that fail on unexpected calls. Compare ordered external method/body traces, user-visible strings/entities/buttons, accepted/ignored ingress, effect IDs/states/receipts, job scheduling/retry state, mappings and control checkpoints. Compare state after each durable transition and after reopening, not just final messages. A test adapter may expose candidate workflow functions to the Bun node:test harness; pure cljs.test can test local rules. No permanent test framework dependency is necessary.

Nondeterminism policy:

| Value | Treatment |
|---|---|
| Clock, TTL/due, cron minute, Status expiry, history day | Inject exact milliseconds and timezone; test boundaries; never strip away timing logic |
| UUID, random chat request ID | Inject deterministic sequence; test collisions; preserve referential reuse |
| Telegram/WhatsApp response IDs | Script responses; if live record aliases, use stable bijection preserving bot/chat/thread scope |
| Tokens, auth keys | Synthetic credentials; calculate exact HMAC/idempotency on synthetic values |
| AES nonce/ciphertext | Fixed entropy in isolated test or semantic decrypt/format comparison; never require real ciphertext equality |
| Media paths | Alias only random path prefix; compare bytes/hash, basename extension, MIME, permissions and containment |
| JSON objects | Ignore wire key order only where semantic; preserve raw bytes/order for HMAC and JSON-hashed durable IDs |
| Date and locale rendering | Pin runtime/ICU, explicit timezone; compare UTF-16 lengths/entities, including astral characters |

Do not normalize away bot choice, API ordering, missing versus null fields, failed versus uncertain, effect keys or duplicate deliveries. These are behavioral differences.

## Required test layers

- Pure golden comparisons: command recipient parsing, actual mode transitions, aliases, entity concatenation/splitting/atomic dates, history DAG ordering and cycle handling, reaction equivalents/echo and specialized callback validation. Include malformed/unknown event and future/invalid timestamps.
- SQLite contracts: all schema keys/indexes, WAL opening/recovery, next-job selection/due/priority/stream blocking/history hold, prepared-statement binding/number types, transaction rollback, sealing/tamper detection, cross-runtime open/write/read. Exercise old exports including n8n jobs and legacy presentation/history versions.
- Transport contracts: loopback fake providers exercising exact OpenWA paths, headers, query options and request bodies; Telegram JSON result envelope and multipart byte/entity integrity; 2 MiB raw ingress, HMAC timing/auth, unknown bots/non-owner filtering. Include 429 Retry-After, connection refusal, timeout after mutation, invalid response, unknown idempotency outcome, read-only method classification and safe reason extraction without provider text leakage (`test/http.test.ts`).
- Registration: unknown contact→topic→create link→Managed Bot event→token encryption/webhook→explicit binding→start→DM/retirement. Include multiple pending contacts, 20-button cap, repeat/token rotation, unbound start, already bound bot, provider creation refusal and partial setup failure. The real feature uses official Managed Bots throughout.
- Routes/groups: exact topic versus whole-group reply threads, mirrored author bot present/absent, own sends, owner replies and quoted wrong-chat refusal, shared/General retirement guards, 403 fallback, missing topic repair before/after partial delivery, picker expiry/reuse/changed route, unmirror permissions/history/stale notification.
- Media: every incoming/outgoing class, unavailable/oversize/stream-over-cap, symlink escape, native rejection and original fallback, sticker label partial retry, frozen byte regeneration, archived media reuse; verify conversion output visually and by alpha/frame/duration/size. Run existing test:stickers inside target image; keep actual external codec fixture assertions.
- Ancillary parity: edits including denied/empty/late/outbound echoes, revoked-before-original/history suppression and own Keep/Delete, reaction full snapshots/custom emoji/refused bot expiry, poll selection toggling and deliberate poll_answer no-op, Status mute revisions/read-only/refresh race, metadata roster validation/photo allowlist/missing permissions/quiet cron, TTL/clear cutoff and undeletable batch fallback.

## Crash and restart matrix

Inject process death before/after enqueue commit, claim, effect sending, remote acceptance, receipt commit, copy mapping and completion; at every history item/day/checkpoint and route/binding notification boundary. Reopen with recovery and assert done effects never repeat, sending becomes uncertain, failed/uncertain blocks only relevant stream, missing IDs remain uncertain and explicit reviewed retry resets only intended effects. Read receipt and cleanup failures must not replay sends. Test forced shutdown during 120-second operations and separate child process-group kill, including filesystem cleanup.

Record API rejection and delivery ambiguity as separate cases: blanket retry is a correctness bug. Run expiry and restart against a temporary file database, not only memory. Soak test one live worker with synthetic HTTP providers, realistic attachments and backlog; measure latency, RSS, event-loop delays and growth of permanent job/effect/message tables. Existing code has no queue pruning policy, so the rewrite must not invent retention changes.

## Live verification and acceptance

No capture or live canary was performed in this investigation. Later use sanitized authorized recordings; webhook traces must include deployed fork/version. Offline replay cannot establish Telegram picker UI, Managed Bot provisioning permission or real media provider behavior. After all offline gates, use a controlled single bridge and owner-approved test conversations to verify those U items. Preserve latest receipts for rollback. Acceptance is zero unexplained differences, all inventory scenarios covered and all target-image checks passed; every exception requires explicit review before cutover.
