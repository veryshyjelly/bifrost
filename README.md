# Bifrost

A self-hosted WhatsApp–Telegram bridge written in Go. Bifrost connects OpenWA and a
local Telegram Bot API server with durable routing, delivery and recovery state
in PostgreSQL.

Bifrost supports Docker Compose and optional Android hosting through Termux.
The setup guides describe installation and operation on either type of host.

## What it does

- Relays text, images, video, voice, documents and static or animated stickers.
- Routes conversations through management topics, contact bots and mirror groups.
- Supports replies, mentions, edits, reactions, polls, deletion choices and history.
- Archives Status posts and schedules resumable metadata refreshes.
- Freezes delivery requests and receipts so retries preserve their original scope.
- Caches successful sticker conversions by source content and destination, including
  across restarts. New stickers need one conversion; repeats reuse verified bytes.

The runtime verifies native identities and callbacks before dispatching ordinary
work. PostgreSQL ownership fencing prevents a second runtime from using the same
account database. Unknown sends remain unresolved for explicit review.

## Docker Compose installation

Install Docker with Compose, then run:

```sh
make configure
# Fill TELEGRAM_API_ID and TELEGRAM_API_HASH in the private .go.env file.
make up
```

Obtain Telegram API credentials from [my.telegram.org](https://my.telegram.org).
`make configure` creates `.go.env` with owner-only permissions, independent database
credentials, encryption and webhook secrets, and a permanent account ID. Repeating
it preserves the existing file. Keep that identity and its keys with every backup.

The stack contains PostgreSQL, Redis, OpenWA, Telegram API and Bifrost. PostgreSQL
uses separate databases and application roles for the bridge and gateway. OpenWA's
browser/authentication configuration also requires its persistent `/app/data`
directory. Redis keeps its queue in append-only persistence.

| Endpoint | Compose default | Phone host |
| --- | --- | --- |
| OpenWA dashboard | `http://127.0.0.1:2786` | `http://127.0.0.1:2785` |
| Bridge readiness | `http://127.0.0.1:3211/readyz` | `http://127.0.0.1:3210/readyz` |
| PostgreSQL | `127.0.0.1:5433` | `127.0.0.1:5432` |

Host listeners bind loopback. Applications inside Compose use their service names
and internal ports. Database initialization runs only for an empty volume;
changing an environment password does not rotate existing database credentials.

For a new installation, retrieve the dashboard bootstrap key locally:

```sh
docker compose -p bifrost-go --env-file .go.env exec openwa cat /app/data/.api-key
```

Pair WhatsApp in the dashboard and create a scoped API key. Fill the session/key
and Telegram owner, master bot and management destination fields in `.go.env`.
Set `BIFROST_COMMAND=run` and `BRIDGE_INGRESS_ENABLED=true`, then run `make up`
again. The initial `serve` mode provides passive diagnostics while configuring
providers. Explicit `run` starts the relay, scheduler and cleanup workflows.

OpenWA builds the [maintained fork](https://github.com/veryshyjelly/OpenWA) at a
reviewed full commit pin, retaining its media and mention fixes. Set
`OPENWA_SOURCE=../OpenWa` only when deliberately building a sibling checkout.

Use `make logs` to follow the bridge and gateway. `make down` retains volumes.
Keep paired engines stopped on any retired host; separate database copies cannot
coordinate runtime ownership with each other.

## Termux phone hosting

Follow the [Termux setup and operations guide](docs/PHONE-HOST.md) for Android
prerequisites, Debian/provider artifacts, automatic startup, backups and recovery.
It includes USB and wireless ADB access, SSH into Termux, dashboard login and the
Android 12 process-limit setting needed for the service stack.

With USB debugging authorized on the phone, forward the dashboard to the Mac:

```sh
adb devices
adb forward tcp:12785 tcp:2785
```

Open [localhost:12785](http://127.0.0.1:12785), log in with the private OpenWA key
and select the Bifrost session. Its live page refreshes pairing QR codes. The relay
runs independently of USB; the forwarded dashboard needs the debugging connection.

## Development

Use the Go version pinned in [go.mod](go.mod), currently 1.27.4. The project uses
pgx/v5, explicit SQL/sqlc and embedded Goose migrations. Provider clients, codec
dependencies and tools are pinned.

```sh
make check
make spec-check
make generate
```

`make check` runs formatting, vet, race-enabled Go tests, the private-file audit,
Compose validation and generated-query drift checks. Database integration tests
need an isolated PostgreSQL instance and create/drop unique test schemas:

```sh
BIFROST_TEST_DATABASE_URL='postgres://test_user:TEST_PASSWORD@localhost:5433/test_db?sslmode=disable' go test -race ./internal/postgres
```

Never point tests at the live database. Without that variable, database integration
tests skip explicitly. Codec tests additionally require
`BIFROST_TEST_CODECS=1`, the pinned Pillow/ffmpeg environment and Chromium; see
[sticker behavior and limits](docs/STICKER-LIMITS.md).

Read the [behavior contract](openspec/README.md), [runtime lifecycle](docs/RUNTIME.md),
[CLI commands](cmd/bifrost/README.md), [identity and mentions](docs/IDENTITY-MENTIONS.md),
[history and attachments](docs/HISTORY-ATTACHMENTS.md) and
[recovery controls](docs/TOPIC-RECONCILIATION.md). Private environments, databases,
media, credentials and backups do not belong in Git.

For full-history privacy checks and repository publication, see
[public repository checks](docs/PUBLISHING.md).

## License

Bifrost is available under the [MIT License](LICENSE). Bundled third-party code
retains its accompanying license notices.
