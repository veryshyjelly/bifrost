# Bifrost

A personal WhatsApp–Telegram bridge being ported to Go. The [approved OpenSpec contract](openspec/README.md) preserves existing routing, linked-name formatting, deletion choices and delivery safety, with conditional caption clearing when the released provider API permits it.

The first Go slices implement PostgreSQL migrations, permanent identity/key checks, durable queue claims, effect receipts, reviewed recovery, typed provider clients, authenticated durable event admission, a fenced delivery worker, initial inbox/target routing and diagnostic HTTP/CLI checks. **WhatsApp/Telegram forwarding and legacy-state import are not implemented yet.** The HTTP status deliberately reports `configured: false` until those workflows exist. The [worker and initial routing](docs/WORKER.md) are tested components; `serve` does not automatically dispatch provider work yet.

## Complete Compose setup

```sh
make configure
# Fill TELEGRAM_API_ID and TELEGRAM_API_HASH in private .go.env.
make up
```

`make configure` creates `.go.env` with mode 0600, independent database credentials, encryption/admin/webhook secrets and a permanent account ID. Repeating it preserves the existing file. It does not read or modify the old installation's private files. Obtain Telegram API credentials from my.telegram.org.

`make up` builds the images, starts PostgreSQL/Redis, explicitly applies Bifrost migrations, then starts Bifrost, OpenWA and the local Telegram API. Everything is owned by the `bifrost-go` Compose project:

| Service | Role |
| --- | --- |
| postgres | One PostgreSQL instance, separate `bifrost` and `openwa` databases and non-superuser accounts |
| redis | OpenWA queue/cache, persistent append-only storage and noeviction policy |
| openwa | WhatsApp gateway and dashboard, PostgreSQL message storage, Redis, persistent browser/auth/media data |
| telegram-api | Official local Telegram Bot API server and persistent file cache |
| bifrost | Go diagnostic runtime and delivery persistence foundation |
| migrate | Explicit one-shot Goose migration command, under the tools profile |

Database bootstrap runs only on an empty PostgreSQL volume. App roles cannot connect to each other's databases and never receive the PostgreSQL administrator password. Changing an environment password later does not rotate database credentials automatically. No external Docker network, pre-existing OpenWA checkout or socket proxy is required.

OpenWA builds [your maintained fork](https://github.com/veryshyjelly/OpenWA) at reviewed commit `6ef0ab6e9` (upstream 0.30.0 plus media/mention fixes), using a full commit pin for reproducible builds. To use the sibling checkout during development, put `OPENWA_SOURCE=../OpenWa` in `.go.env`; this explicitly builds that checkout, including its local changes. OpenWA's upstream release still requires SQLite for boot/auth configuration even when message storage uses PostgreSQL; its entire `/app/data` directory remains persistent. Bifrost runtime state uses PostgreSQL only.

Default host endpoints bind loopback and avoid the legacy bridge/OpenWA ports:

- OpenWA dashboard: `http://127.0.0.1:2786`
- Bifrost diagnostics: `http://127.0.0.1:3211/healthz`
- PostgreSQL: `127.0.0.1:5433` (applications use the internal service address)

On a fresh OpenWA installation, retrieve its bootstrap credential locally when needed:

```sh
docker compose -p bifrost-go --env-file .go.env exec openwa cat /app/data/.api-key
```

Pair WhatsApp through OpenWA and create a scoped API key. Record the session/key and Telegram owner/master/management fields in `.go.env` for the later provider implementation. This phase does not register callbacks or send provider operations. Do not pair the live bridge identity into a second concurrently running runtime.

`make down` stops this project without deleting volumes. Never use `down -v` on an installation whose identity or delivery state must be preserved.

## Development and validation

Use Go 1.27.3. PostgreSQL uses pgx/v5, explicit sqlc queries and embedded Goose migrations. Dependencies and the sqlc tool are pinned in go.mod/go.sum. Run:

```sh
make check
make spec-check
make generate
```

`make check` includes formatting, vet, race-enabled tests, repository auditing, Compose validation and generated-query drift detection. PostgreSQL integration tests require an explicitly isolated URL and create/drop unique test schemas:

```sh
BIFROST_TEST_DATABASE_URL='postgres://bifrost:YOUR_TEST_PASSWORD@localhost:5433/bifrost?sslmode=disable' go test -race ./internal/postgres
```

Use a disposable test instance, never the live database. CI runs these tests against PostgreSQL. Without the variable, those integration tests are explicitly skipped. Go tests verify only the implemented slice; the historical 263 passing tests are reference evidence for remaining parity work.

The CLI also supports `migrate`, `serve`, `healthcheck` and explicit read-only `check-openwa` / `check-telegram` diagnostics. See [provider checks](docs/PROVIDER-CHECKS.md) for authorized test sends and live evidence. It reads environment variables directly; Compose supplies them from `.go.env`. It never automatically sources the legacy `.env` or `bridge.env`. HTTP implements `/healthz`, admin-protected `/v1/status` and reviewed `/v1/review`. Authenticated durable OpenWA/Telegram ingress and signed `/v1/management` are available with explicit `BRIDGE_INGRESS_ENABLED=true`; see [ingress setup and limits](docs/INGRESS.md). Admission does not enable automatic routing or register provider callbacks.

## Existing installations

The original TypeScript implementation and tests remain at `checkpoint-typescript-baseline`; [historical deployment/recovery docs](docs/legacy/README.md) are retained. Existing private files, SQLite state, media, backups and Docker volumes are unchanged. Do not point this partial Go runtime at a legacy installation or mint replacement identity/keys for existing state. An explicit importer and validated cutover are required before migration. See [OpenWA migration requirements](docs/OPENWA-MIGRATION.md) before moving the existing gateway’s saved settings and paired profiles.

Read [the implementation design](docs/GO-IMPLEMENTATION.md), [validation evidence](docs/VALIDATION.md) and [MCP setup](docs/MCP-SETUP.md). Python helpers under `scripts/` are retained for later codec/import work; they are not part of the Go runtime yet.
