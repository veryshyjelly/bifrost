# Go port completion checklist

The Go runtime composes the implemented workflows. Preserve the approved OpenSpec contract and checkpoint `checkpoint-typescript-baseline`. [CUTOVER.md](CUTOVER.md) describes host-transfer and recovery precautions.

The owner has approved an empty Go queue at migration: old pending/failed/history jobs do not need to resume, and group history will be imported again afterward. Keep the original archive and uncertain-send evidence without replaying or marking those sends confirmed. The critical path is retained identity/routes/bindings/confirmed state, asset restoration, complete runtime wiring, rehearsal and cutover. Original pending-job/history compatibility is no longer a cutover prerequisite under the explicit `fresh` policy.

- [x] Managed Telegram bot credentials, native registration, local API migration and callback setup in isolated fixtures.
- [x] Contact provisioning, scoped binding choices and conditional inbox retirement in isolated fixtures.
- [x] History choices, frozen ordered imports, destination receipts and scoped live holds in isolated fixtures.
- [x] Attachment delivery, captions/mentions, durable media archives and sticker codecs/validation in isolated fixtures.
- [x] Mapped text/caption edits, edit echoes, out-of-order source edits and independent refusal notices in isolated fixtures.
- [x] Scoped native poll controls, serialized confirmed selections, source vote notices and crash-safe vote retries in isolated fixtures.
- [x] Actor-specific native reactions, scoped custom emoji, retained-copy projection, fallback notices and crash-safe setters in isolated fixtures.
- [x] Source tombstones, destination-scoped owner Delete/Keep choices, expiry and resumable cleanup in isolated fixtures.
- [x] Explicit /react and /delete commands, authenticated management actions and idempotent source-operation recovery in isolated fixtures.
- [x] Guarded route repair, frozen destination replacement and topic-creation crash recovery in isolated fixtures.
- [x] Passive Status archive, frozen refreshes, alias-scoped author mutes and versioned controls in isolated fixtures.
- [x] Operational status/inbox commands, passive session notices and atomic delivery-problem admission in isolated fixtures.
- [x] Explicit scoped topic inspection/reconciliation and authenticated recovery CLI in isolated fixtures.
- [x] Read-only legacy SQLite export/validation and transactional PostgreSQL import preserving identity, keys, receipts, mappings and uncertain-send evidence under the approved fresh-queue policy; restored assets verified before activation.
- [x] Complete runtime dependency composition, callback registration, scheduler, worker and startup recovery, gated from diagnostic serve, in isolated native/PostgreSQL lifecycle tests.
- [x] Native OpenSpec scenario regressions, isolated PostgreSQL/concurrency/recovery tests, strict specification validation, final image/Compose/codec checks and cutover documentation.
- [x] Final stopped-writer backups, restored shared PostgreSQL/OpenWA/Redis/Telegram state, native bot setup and confirmed incoming live relay; original services stopped and retained.

Capability evidence comes from isolated native SDK/PostgreSQL and codec fixtures. Operators should verify provider readiness and message delivery on their own installation and keep those records private.

Provider versions verified for this implementation: OpenWA Go SDK `v0.11.1`, go-telegram/bot `v1.28.0`. The reviewed OpenWA gateway commit remains unchanged. No private environment, legacy database, media archive or permanent Docker volume is disposable.
