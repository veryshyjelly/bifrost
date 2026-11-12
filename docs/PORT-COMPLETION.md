# Go port completion checklist

The user requested completion of all remaining workflows, legacy import and runtime composition. Preserve the approved OpenSpec contract and checkpoint `checkpoint-typescript-baseline`; do not enable a live cutover during preparation.

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
- [ ] Passive Status archive, refresh, author mutes and scoped controls.
- [ ] Remaining status/operational owner commands and explicit transition reconciliation.
- [ ] Read-only legacy SQLite export/validation and transactional PostgreSQL import preserving identity, keys, receipts, mappings and uncertain sends.
- [ ] Complete runtime dependency composition, callback registration, scheduler, worker and startup recovery, gated from diagnostic serve.
- [ ] Full OpenSpec acceptance coverage, isolated PostgreSQL/concurrency/recovery tests, final image/Compose checks and safe cutover documentation.

Provider versions verified for this implementation: OpenWA Go SDK `v0.11.1`, go-telegram/bot `v1.28.0`. The reviewed OpenWA gateway commit remains unchanged. No private environment, legacy database, media archive or permanent Docker volume is disposable.
