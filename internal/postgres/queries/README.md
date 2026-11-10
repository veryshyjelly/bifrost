# PostgreSQL delivery queries

`delivery.sql` is the source for sqlc-generated pgx/v5 methods under `internal/postgres/db`. Regenerate with `make generate`; do not edit generated files.

Admission uses a transaction-scoped stream lock before insertion. Claims use SKIP LOCKED plus an unfinished-stream-head guard. Receipt/state writes lock and validate the active claim token and expiry. Provider calls must happen outside transactions; claim renewal is the worker's responsibility. Expired sends become uncertain, while expired jobs with no ambiguous effects can be retried.

Complete native WhatsApp admission also freezes a source snapshot and resolves its stable conversation stream atomically. Source preparation and claims share a short topology lock so recovery cannot move a raw job past an active canonical claim. The claim guard excludes active jobs regardless of their relative ID. Older raw jobs retain their IDs/keys and release their token when deferred; no provider call runs while normalizing or holding these locks.
