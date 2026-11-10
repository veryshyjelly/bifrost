# Status and metadata baseline

OpenSpec task 1.9 records **33 cases and 414 observed transitions** against immutable baseline `checkpoint-clojure-baseline`: all 17 original Status assertions, all 15 original metadata/cron assertions and one supplemental quiet scheduled run. Run `bun scripts/rewrite/status-metadata-baseline.ts check`. Two independent complete transcripts must match each other and the committed [manifest](../../test/rewrite/fixtures/status-metadata-baseline.json). Full API requests/results/errors, logs and raw durable tables regenerate at ignored `data/rewrite/evidence/status-metadata-baseline.json`.

| Coverage | Immutable inventory sources |
|---|---|
| Single archive topic, linked author/native time, startup/webhook refresh deduplication, read-only replies, omitted/status media, mute-before-download, refresh/unmute races and skipped-story suppression | Original `test/statuses.test.ts` |
| Persisted muted preferences across real SQLite reopening, identity aliases, pagination, owner/bot/session/chat/topic/expiry controls and newer-decision protection | Original `test/statuses.test.ts` |
| Group/contact names and descriptions, participant roster validity, missing-bot controls, topic versus mirror scope, read-only WhatsApp metadata, missing permissions and scoped grants | Original `test/sync.test.ts` |
| Content-hash photo caching despite rotating signed URLs, hidden photo/description preservation, alias/bot sharing, failed lookups, request coalescing and chat-job priority | Original `test/sync.test.ts` |
| Timezone-aware persisted cron-minute deduplication and supported/malformed schedule expressions | Original `test/sync.test.ts` |
| A populated group's second unchanged cron run completes without another notice | Supplemental [metadata-scenarios.ts](../../scripts/rewrite/metadata-scenarios.ts), exercising original queue, worker and quiet-report branch |

Original source hashes and inventory IDs are retained for all 32 original assertions. The picture-cache case uploads an actual four-byte synthetic file; observation retains its exact bytes, hash, mode and `profile.jpg` name. Only the random `hermes-sync-*` temporary parent beneath the real OS temp directory is aliased to `$PHOTO-1`. Arguments passed into original provider fixtures remain unchanged. Actual media/codec quality is separate evidence.

The clock injection fixes both `Date.now()` and argument-free `new Date()` for this family; explicit dates, date parsing and timezone behavior remain native. This is necessary because the original refresh fixture constructs ISO timestamps with `new Date()`. Earlier families retain their previous observation settings/digests. Empty drain ticks are omitted only after checking no API call/error and byte-identical state, as documented for [history](history-baseline.md). Fetch rejects provider networking, and Linux verification disables networking independently.

Candidate Status/metadata implementation and replay, real provider verification and all eight release gates remain pending. No production webhook, profile or mute preference was read or changed.
