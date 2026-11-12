# Bifrost CLI

`configure` creates private Compose configuration without overwriting keys. `migrate` applies embedded PostgreSQL migrations. `serve` exposes diagnostics, reviewed recovery and opt-in authenticated durable ingress (docs/INGRESS.md); automatic provider relaying is not implemented yet. `healthcheck` probes the local endpoint. `check-openwa` and `check-telegram` perform read-only provider checks by default; explicit authorized test sends require a target, text and stable key. See the root README and docs/PROVIDER-CHECKS.md.

`legacy-validate --file private-export.tar.gz` validates an existing legacy export in fresh private staging, including credentials, SQLite/WAL integrity, durable state and identity relationships. It requires Python with sqlite3 and does not connect to PostgreSQL or providers. It prints only an account/count/digest summary. See [LEGACY-IMPORT.md](../../docs/LEGACY-IMPORT.md); import and runtime compatibility remain pending.
