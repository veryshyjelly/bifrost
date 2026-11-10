# Media, structured content and transport baseline

OpenSpec task 1.5 records **73 media/structured scenarios and 27 additional transport scenarios** against immutable baseline `checkpoint-clojure-baseline`. This supplements the existing 80 adapter comparisons and original default suite. It records original behavior; candidate media implementation and release gates remain pending.

Run `bun scripts/rewrite/media-baseline.ts check` to replay two independent original processes, compare their full outputs and check the committed [manifest](../../test/rewrite/fixtures/media-baseline.json). Full ordered requests, raw persisted JSON, all six SQLite tables, returned attachment metadata, file bytes/hashes/permissions and per-transition outcomes are regenerated at ignored `data/rewrite/evidence/media-baseline.json`. `record` explicitly replaces the manifest; do not use it to accept unexplained differences. The regression test checks the manifest and safety assertions without writing to the workspace, including in the read-only Linux image.

The manifest stores separate SHA-256 digests of exact serialized requests, state and outcomes after every step. It retains readable ordered method names and errors. The complete transcript is reproducible from the checked-in [inputs](../../scripts/rewrite/media-scenarios.ts), rather than repeated in a large checked-in dump. Differences in JSON field order, missing/null, raw hashed payloads, request order, receipt scope and uncertainty change these digests.

| Scenario group | Recorded behavior | Original inventory references |
|---|---|---|
| Incoming native media | Image/video/audio/voice/sticker/document, exposed view-once bytes, WebM sticker filename, captions, participant bot presentation | `test/bridge.test.ts`: 117, 183, 358, 368 |
| Conversion and fallback | Voice/image conversion, unsupported stickers, untouched original bytes after definitive native refusal, long labels, no fallback for uncertain or topic/reply refusal | `test/bridge.test.ts`: 384, 419, 523 |
| Outgoing native media | Photo/video/video note/animation/audio/voice/sticker/document; TGS/WebM inputs, MIME, filenames, quotes, idempotency and separate read work | `test/bridge.test.ts`: 400, 419, 435 |
| Retry and archive reuse | Accepted sticker label retained; frozen conversion choice; same-byte regeneration accepted, changed bytes refused; missing receipt remains uncertain; archived Telegram media reuse and selected source fallback | `test/bridge.test.ts`: 435, 523; `src/bridge.ts`: 390, 630 |
| Structured messages | Location, valid/malformed vCard, poll creation with 12-button cap and callback-to-row identity, unsupported messages; both directions | `test/bridge.test.ts`: 183, 452 |
| Download and filesystem | Authenticated encoded OpenWA paths, configured Telegram cloud endpoint, inline/declared/header/stream limits, exact cap, partial-file cleanup, interrupted streams, timeout/refusal, shared files and symlink escape | `test/http.test.ts`; `src/media.ts`: 16, 22, 35, 45 |
| Transport additions | Multipart and OpenWA read/mutation: header/envelope 429, unknown idempotency outcome, 500/501, invalid JSON, connection refusal, timeout and safe reason extraction | `test/http.test.ts`: 47, 70; `test/rewrite/api.test.ts` |

Each case starts with a fresh synthetic database, fixed UTC clock, deterministic UUID sequence and scripted receipts/errors. Only the temporary filesystem root is aliased to `$FIXTURE`; UUID relationships, file extensions, bytes and durable identifiers are retained. Bun's native module mock injects UUIDs before importing original code. The `/telegram-data` root lookup maps to a disposable directory; original `realpath` still resolves every returned file and symlink before original containment and size checks. No host shared mount is accessed.

Provider methods and downloads fail closed when unscripted. Download, storage, stream-limit, byte-read, routing and fallback logic run from the original source. Conversion/sticker outputs are deliberately scripted at the codec boundary. Actual codec output, visual/alpha/frame/duration checks, process isolation and the nine opt-in codec tests remain Gate 7 work. Poll selection/reactions/edits/revocation recordings belong to task 1.7. This evidence does not authorize live provider access or deployment.

The independent baseline imports and original tests are unchanged. The subprocess flushes its transcript after closing all databases before exiting; it is a recorder, not lifecycle/shutdown evidence.
