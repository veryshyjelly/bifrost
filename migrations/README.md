# PostgreSQL migrations

Goose migrations are embedded and applied explicitly by `bifrost migrate`.
`make up` runs that command after database readiness. Application startup does not
apply schema changes or reset another worker's claims.

Applied files are immutable history. Retain their table, column, trigger and
constraint names even when the tooling that first populated them has been retired.
The runtime's read-only retained-state adapters still use scoped activation,
credential, deduplication, completion and suppression evidence. Do not drop those
records, rewrite receipts or rename identifiers to match presentation terminology.

There are no destructive Down migrations. Add new schema changes as forward
migrations, regenerate SQL adapters with `make generate` and run isolated
PostgreSQL tests. `make check` verifies generated-query drift.
