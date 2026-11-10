# PostgreSQL migrations

Goose migrations are embedded and applied only by `bifrost migrate`. `make up` invokes that explicit command after database readiness. Application startup never applies schema changes or resets another worker's claims.

Migration 00001 establishes permanent identity/key fingerprint, jobs and effect receipts. There is deliberately no destructive Down migration. This is the first implementation slice, not the complete legacy import schema. Preserve original SQLite snapshots until a separately tested importer/cutover exists.

SQL changes require regenerating sqlc queries with `make generate`; `make check` detects generated-code drift.
