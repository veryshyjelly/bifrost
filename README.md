# Bifrost

A personal WhatsApp–Telegram bridge implemented in Go. The [approved OpenSpec contract](openspec/README.md) preserves existing routing, linked-name formatting, deletion choices and delivery safety, with conditional caption clearing when the released provider API permits it.

The Go installation has replaced the legacy runtime. PostgreSQL retains the original identity, encrypted credentials, routes, bindings, confirmed copies and interaction state; media and Telegram files were restored and verified before activation. Old pending jobs and unfinished history remain archived under the owner's approved fresh-queue policy. See [cutover evidence and operations](docs/CUTOVER.md).

Explicit `run` composes authenticated ingress, fenced delivery, text/media, commands, managed bots, mirrors, history, edits, reactions, polls, revocations, Status, metadata scheduling and cleanup. It verifies native provider identities and callbacks before reporting readiness or dispatching ordinary work. Diagnostic `serve` remains passive. See [runtime lifecycle](docs/RUNTIME.md), [completion checklist](docs/PORT-COMPLETION.md) and [validation evidence](docs/VALIDATION.md).

Routing supports management topics, contact bots and linked mirror groups, including `/unmirror` and intentional former-topic deletion. [Identity and mentions](docs/IDENTITY-MENTIONS.md), [history and attachments](docs/HISTORY-ATTACHMENTS.md), [metadata runs](docs/METADATA-RUNS.md) and [sticker limits](docs/STICKER-LIMITS.md) document the retained behavior and recovery rules.

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
| bifrost | Go bridge runtime, authenticated ingress and durable delivery |
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

For a fresh installation, pair WhatsApp through OpenWA, create a scoped API key and fill the session/key and Telegram owner/master/management fields in `.go.env`. Set `BIFROST_COMMAND=run` and `BRIDGE_INGRESS_ENABLED=true`, then run `make up` again. The initial default `serve` lets you configure providers before enabling forwarding. Existing installations must follow [legacy import](docs/LEGACY-IMPORT.md); never generate replacement identity or keys over retained state or run two paired engines.

The migrated installation overrides the default ports: OpenWA uses `http://127.0.0.1:2785`, and Bifrost readiness uses `http://127.0.0.1:3210/readyz`. Use `make logs` to follow its bridge and gateway logs.

`make down` stops this project without deleting volumes. Never use `down -v` on an installation whose identity or delivery state must be preserved.

## Development and validation

Use Go 1.27.4. PostgreSQL uses pgx/v5, explicit sqlc queries and embedded Goose migrations. Dependencies and the sqlc tool are pinned in go.mod/go.sum. Run:

```sh
make check
make spec-check
make generate
```

`make check` includes formatting, vet, race-enabled tests, repository auditing, Compose validation and generated-query drift detection. PostgreSQL integration tests require an explicitly isolated URL and create/drop unique test schemas:

```sh
BIFROST_TEST_DATABASE_URL='postgres://bifrost:YOUR_TEST_PASSWORD@localhost:5433/bifrost?sslmode=disable' go test -race ./internal/postgres
```

Use a disposable test instance, never the live database. CI runs these tests against PostgreSQL. Without the variable, those integration tests are explicitly skipped. Historical checkpoint tests and OpenSpec scenarios provide the behavior reference; native tests cover retries, concurrency, scope checks, partial delivery and recovery.

The CLI supports `migrate`, `serve`, `run`, `healthcheck`, provider diagnostics, legacy validation/preparation/activation and authenticated recovery. See [provider checks](docs/PROVIDER-CHECKS.md), [ingress](docs/INGRESS.md) and [topic reconciliation](docs/TOPIC-RECONCILIATION.md). It reads environment variables directly; Compose supplies them from `.go.env` and never automatically sources the legacy private files. HTTP exposes liveness, readiness, admin-protected status and reviewed recovery.

## Existing installations

The original TypeScript implementation and tests remain at `checkpoint-typescript-baseline`; [historical deployment/recovery docs](docs/legacy/README.md) are retained. The completed migration keeps original containers, volumes, configuration and final backups. They must remain stopped while Go owns the live identity. See [legacy import](docs/LEGACY-IMPORT.md), [OpenWA migration](docs/OPENWA-MIGRATION.md) and [cutover operations](docs/CUTOVER.md) before attempting another move or rollback.

Read [the implementation design](docs/GO-IMPLEMENTATION.md) and [MCP setup](docs/MCP-SETUP.md). The Go image includes the Python/Pillow codec environment and embedded Lottie renderer; `scripts/transfer-volume.py` supports consistent archival and restoration.
