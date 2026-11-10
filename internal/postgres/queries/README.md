# PostgreSQL delivery queries

`delivery.sql` is the source for sqlc-generated pgx/v5 methods under `internal/postgres/db`. Regenerate with `make generate`; do not edit generated files.

Admission uses a transaction-scoped stream lock before insertion. Claims use SKIP LOCKED plus an unfinished-stream-head guard. Receipt/state writes lock and validate the active claim token and expiry. Provider calls must happen outside transactions; claim renewal is the worker's responsibility. Expired sends become uncertain, while expired jobs with no ambiguous effects can be retried.
