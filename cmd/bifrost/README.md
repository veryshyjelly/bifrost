# Bifrost CLI

`configure` creates private Compose configuration without overwriting keys. `migrate` applies embedded PostgreSQL migrations. `serve` exposes diagnostics and reviewed recovery; automatic provider relaying is not implemented yet. `healthcheck` probes the local endpoint. `check-openwa` and `check-telegram` perform read-only provider checks by default; explicit authorized test sends require a target, text and stable key. See the root README and docs/PROVIDER-CHECKS.md.
