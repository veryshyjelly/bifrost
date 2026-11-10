# Desired behavior review

Status: **approved**. Preserve the specified behavior, including intentional deletion rules and linked-name formatting. All eleven baseline capabilities are approved for preservation, with one conditional behavior change: support clearing an outgoing caption when the latest released provider API permits it.

## Decisions

| Group | Approved target | State |
| --- | --- | --- |
| Routing | Preserve every route type, per-person bot identity, group linking, intentional former-topic deletion and /unmirror topic recreation. History remains explicitly fetched with /history. | Approved |
| Messages | Preserve sends, quotes, linked-name formatting, mentions, polls, reactions, automatic removal of other authors’ revoked messages and Delete/Keep choices for owner messages. Add conditional caption clearing. | Approved |
| Archives | Preserve native media, sticker conversion/fallback, explicit history modes, retained date headings and passive Status archive with author mutes. | Approved |
| Delivery | Preserve ordered durable queues, deduplication, partial receipts, stable idempotency, restart recovery, explicit uncertain-send review and read-receipt behavior. | Approved |
| Operations | Preserve authenticated ingress, owner-only controls, configuration behavior, sync, schedules, cleanup and identity transfer. Use PostgreSQL and Docker Compose for the Go bridge; retain existing external OpenWA connectivity. | Approved |

## Approved differences from the current implementation

- Use the required Go provider libraries at the latest stable releases and use applicable current native features.
- Replace SQLite runtime persistence with PostgreSQL while preserving identity and delivery state.
- Use Docker Compose for the Go setup, PostgreSQL and local Telegram API.
- Permit clearing mapped outgoing media captions when the selected latest released provider API supports it; otherwise retain the current caption and explain the limitation.

No other behavioral changes are approved. Relevant new API features may improve implementation without changing these semantics; new user-visible behavior requires a separate explicit change.

## Verification deferred to implementation design

Release-level SDK/API coverage, PostgreSQL version/driver/schema, transactional job claiming, migration/cutover/rollback and backup tooling need verification and a separate implementation design. In particular, a Go string field or an edit method alone does not prove that the provider supports clearing a caption. Verify the released request contract and a real empty-caption edit, including serialization of an explicitly empty value. These are implementation checks, not unresolved product decisions.

The current SQLite deployment is unchanged by this documentation phase.
