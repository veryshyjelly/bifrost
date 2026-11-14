# Bifrost CLI

- `configure` creates private Compose configuration without replacing existing keys.
- `migrate` applies the embedded PostgreSQL migration history.
- `serve` provides passive diagnostics, reviewed recovery and opt-in durable ingress.
- `run` verifies native identities/callbacks and starts the fenced relay, scheduler and cleanup.
- `healthcheck` probes the configured local listener.
- `check-openwa` and `check-telegram` perform read-only provider checks by default.
- `review`, `inspect-topic` and `reconcile-topic` expose authenticated recovery controls.

Explicit provider test sends require a destination, text and stable key. The CLI
reads literal environment variables; Compose supplies private `.go.env` values.
See [configuration](../../README.md), [runtime lifecycle](../../docs/RUNTIME.md),
[provider checks](../../docs/PROVIDER-CHECKS.md) and
[topic recovery](../../docs/TOPIC-RECONCILIATION.md).
