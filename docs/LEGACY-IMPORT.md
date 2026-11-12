# Legacy state preparation

The TypeScript checkpoint is `checkpoint-typescript-baseline`. Its export format includes `manifest.json`, `bridge.env`, `telegram-api.env`, `data.tar.gz`, `media.tar.gz` and `telegram-data.tar.gz`. The export contains credentials and private messages. Keep it private and retain the original source resources.

The Go CLI can inspect an existing, frozen export without Docker, PostgreSQL, provider access or runtime configuration:

```sh
bifrost legacy-validate --file /private/path/bifrost-export.tar.gz
```

This validates the archive and SQLite snapshot. It does not import, start a worker, change webhooks or perform a cutover. Transactional PostgreSQL import and active compatibility for the remaining legacy journals are still being implemented. Successful validation does not establish runtime readiness.

The command prints the permanent account, archive/SQLite SHA-256 digests, row counts, durable domain counts and job/effect state counts. It omits tokens, encryption keys, provider URLs, raw payloads and errors. `sending` effects and `running` jobs remain unchanged during inspection; the report does not claim they completed or become safe to replay.

Python 3.11 or newer with its standard `sqlite3` module is required; the existing codec runtime image already includes it. The reader uses embedded trusted code in an isolated interpreter, a read-only connection, a transaction, an authorizer and SQLite size limits. It never loads an archived script, runtime or environment. It includes committed WAL data and checks the complete database integrity. A callback receiving rows must wait for the reader's final digest and cross-row validation before committing anything.

The permanent account comes from the saved identity, checked against `BIFROST_ACCOUNT_ID`, then the configured original account or original OpenWA session. A rotated provider connection never replaces a saved account. Telegram API credentials must match both private files, and every saved bot token must decrypt with the original key and match its bot ID. Original encrypted strings and file bytes are preserved. Alias validation uses explicit evidence; a LID display number never becomes a phone identity.

Extraction uses a fresh private staging directory and removes only that directory on completion or failure. Files become `0600`, directories `0700`; archived executable permissions and ownership are ignored. Component hashes, gzip checksums and complete tar streams are checked. Absolute/traversal paths, links, special files, duplicate paths, unexpected outer files, hidden trailing data, concatenated gzip members and conflicting file/directory paths are rejected. No source file or volume is deleted or rewritten.

Default limits are 64 GiB compressed archive, 256 GiB total expanded content including archive metadata, 64 GiB per file, one million archive entries and one million SQLite rows. Use `--max-archive-bytes` and `--max-expanded-bytes` to lower or explicitly increase the corresponding limits for a known export. `--scratch DIRECTORY` chooses the parent of the owned private staging directory. Ensure sufficient free space for retained Telegram and media files.

The semantic snapshot reader recognizes all 55 durable KV domains and 18 current legacy job types. It rejects unknown domains/types, malformed JSON, conflicting alias/bot bindings, unregistered bot references, wrong private/group control scopes and ambiguous group message mappings. It retains exact integer IDs and source JSON strings. It does not fabricate missing provider requests or receipts from the older effect table, which did not store them.

The internal PostgreSQL preparation API is tested separately from activation. Migration 31 stores an archive/account checkpoint, exact append-only rows from all six source tables, exact KV value strings and an encrypted copy of both original environment files. Preparation accepts only an empty business schema with the same permanent account/key, or creates that identity within the same transaction. It serializes identity and queue admission, refuses concurrent destination writers, and commits only after final SQLite/digest/cross-row validation. A late source error, COPY failure or projection error rolls back the complete preparation, including a newly created identity.

The exact same archive can be prepared again without changing its first result or resetting subsequent work. A different archive cannot merge into that checkpoint. Source rows, KV values and checkpoint identity are protected against updates/deletion. Until native journal projection and external asset restoration are complete, the checkpoint has no activation marker: both explicit runtime readiness and the SQL claim query block dispatch for the account. Preparation does not insert runnable legacy jobs or fabricate native effects. This internal API is not exposed as a finished import CLI yet.

Create a source export with the historical procedure in [MOVING.md](legacy/MOVING.md) from the checkpoint. That procedure quiesces the source writers and preserves all original volumes. Do not archive a changing SQLite database by copying its main file alone. OpenWA session state requires its separate documented migration; it is not part of a Bifrost legacy export. No live export, import or identity cutover is performed by these preparation checks.

SQLite behavior references: [read-only WAL databases](https://sqlite.org/wal.html#read_only_databases), [Python read-only URI connections](https://docs.python.org/3/library/sqlite3.html#how-to-work-with-sqlite-uris).
