# Required domain model

Facts below come from `src/store.ts:10`, `src/identity.ts:7`, `src/bridge.ts:47`, `src/history.ts:13`. This is a rewrite design vocabulary, not a replacement schema.

| Term | Identity and relationship | Required invariant |
|---|---|---|
| Bridge account | persisted identity/account.id; one WhatsApp identity per database | Preserve across service session rotation; explicit mismatch aborts startup |
| OpenWA session | configured external transport session ID | Admission must match current session; never use as replacement for persisted account |
| Contact | canonical WhatsApp person ID, contact KV | Phone and LID aliases may resolve same person; conflicts require review |
| WhatsApp conversation | private peer or @g.us group | Different from author in a group; Status uses synthetic status@broadcast |
| Manager bot | special internal key master | Coordinates owner controls and fallback presentation |
| Managed bot | numeric Telegram bot ID + sealed token, username, ready | Binding cannot reuse one bot for another contact; ready is separate from bound |
| Route | KV keyed by canonical chat ID, also carries chatId | Telegram destination/thread, mode and inbox provenance; whole-group route has no required thread |
| Provision request | contact-keyed pending→bound | Explicit contact selection required after Telegram creation |
| Telegram destination | group+thread or private owner+bot | Private IDs and file access are bot scoped; group message scope is shared |
| Message copy | messages primary key bot/tg_chat/tg_id | Many Telegram parts/destinations may represent one WA message |
| Job | permanent unique dedup, stream, payload, due, attempts | A failed/uncertain predecessor blocks stream; ordering is not per bot globally |
| Effect | stable external operation key + state + receipt | done immutable; sending at crash means uncertain, never automatic resend |
| Telegram ledger record | scope/chat/message, sequence, kind, deletion state | Deleting copy preserves source mapping; chat never expires as notice |
| History run | account, chat, frozen target/count/mode/style/plan | One active run per conversation; dedup depends on destination and copy mode |
| Control request | action ID with workflow-specific context | Validation differs by workflow; preserve existing rules |
| Reaction state | source message→actors, desired/applied destination bot reactions | Retain source emoji separately from Telegram approximation and owner echo |
| Revocation | source tombstone + per-destination deletion task | No resurrection during late arrival/history; own-copy choice differs from others |
| Metadata run | account hash, scope, targets, results, waiters | Shared management group identity never overwritten by one WA group |
| Status preference | account+canonical author+revision | Conversation routing unaffected; latest preference wins through aliases |

Route mode facts (`bridge.ts:204`, `835`): absent→inbox or dedicated; binding changes inbox/dedicated→dedicated; ready chooses DM; link→group; mute→muted; ignore→ignored; keep returns group only if current mode is group, otherwise inbox. Thus keep after muting a group sets mode inbox while retaining its stored destination. Characterize this transition before changing it. Under the finalized [implementation contract](08-implementation-specification.md), correct it only if a demonstrated contract/invariant violation warrants a registered, tested exception; ambiguous semantics require escalation. Unmirror retains muted/ignored, otherwise inbox. Fallback may restore management topic without removing dedicated binding.

Queue transitions: pending→running→done/pending/failed/uncertain. Recovery running→pending, effect sending→uncertain. Review retry/skip are explicit operator transitions. History queued→fetching→delivering→complete or failed; selection pending→selected/cancelled/busy. Metadata queued→running→completed/failed, with target warnings allowed. Revocation tasks track separate choice/deletion progress.

Persistence must retain the existing JSON property names, omission semantics, SHA256/HMAC byte input, AES-256-GCM layout (12-byte IV + ciphertext + 16-byte tag, base64), SQL keys and integer representations. Fields historically named session frequently contain the stable account or its digest, not the current OpenWA session. No general account/session field rename during parity migration.

Recommended in-memory data: immutable keyword maps for decisions; JS objects at wire/storage boundaries. Identifier values remain opaque strings; Telegram API numeric fields are converted only where current contract does so. Store credentials in adapter closures, never route/message maps or replay artifacts. Atoms hold only process-local busy/timers, not durable truth.
