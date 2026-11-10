# Current storage and interface reference

This describes the TypeScript baseline, not the future PostgreSQL schema. Source: [store](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [config](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/config.ts), [HTTP handler](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/server.ts), [Compose setup](../compose.yaml).

## SQLite state to preserve

The current database uses WAL and a 5-second busy timeout. Its six durable tables are:

| Table | Meaning and identity |
| --- | --- |
| kv | JSON state by kind/id: permanent identity, routes, bots, preferences, history checkpoints and prepared snapshots |
| jobs | Unique dedup key, ordered stream, payload, state, attempts, due time and error |
| effects | Stable effect ID, pending/sending/done/uncertain state and immutable completed result |
| messages | WhatsApp/Telegram correspondence, keyed by bot + Telegram chat + message; source IDs may map to multiple copies |
| aliases | Provider identity aliases to canonical contacts |
| telegram_messages | Cleanup ledger keyed by scope/chat/message, with bot, topic, expiry, deletion state and errors |

The PostgreSQL port must preserve these meanings and key scopes; this table is not a prescribed SQL schema. Never derive permanent account identity from newly rotated transport credentials. Use the original encryption key to retain encrypted bot token access. Migration must preserve both active and completed history copies and frozen send/media snapshots, not merely conversations and bots.

## Configuration defaults

The current environment names remain compatibility evidence for later review. Private values are omitted.

| Environment variable | Baseline default / meaning |
| --- | --- |
| BRIDGE_PORT | 3210 |
| BRIDGE_DB | ./data/bridge.db (SQLite baseline only) |
| BRIDGE_MEDIA | ./media |
| TELEGRAM_API_URL | http://telegram-api:8081 |
| BRIDGE_CALLBACK_URL | http://bifrost:3210 |
| OPENWA_URL | http://openwa-api:2785 |
| HISTORY_LIMIT | 20, configured range 0–2000; manual count 1–2000 |
| MAX_MEDIA_BYTES | 100 MiB |
| DELIVERY_INTERVAL_MS | 1100, minimum 100 |
| SYNC_CRON | 0 * * * * |
| SYNC_TIMEZONE | Asia/Kolkata |
| COMMAND_TTL_SECONDS | 60 |
| CONTROL_TTL_SECONDS | 600 |

Private operator configuration includes BRIDGE_ENCRYPTION_KEY, BRIDGE_ADMIN_TOKEN, OPENWA_WEBHOOK_SECRET, TELEGRAM_OWNER_ID, TELEGRAM_MANAGEMENT_CHAT_ID, TELEGRAM_MASTER_TOKEN, OPENWA_API_KEY, OPENWA_SESSION_ID and BIFROST_ACCOUNT_ID. Do not add their actual values to specs. PostgreSQL connection configuration is intentionally left to the port design.

## HTTP responses

- GET /healthz returns 200 with status and configured state; GET /v1/status requires admin bearer authorization and returns detailed counts, queue and sync/control status.
- Accepted POST routes return 200 with an accepted boolean. Duplicate events can return accepted=false without being an error.
- Wrong authentication returns 401, unknown routes/bots return 404, invalid JSON/object/event inputs return 400, and an oversized body returns 413.
- POST /v1/review accepts job and action (retry/skip); uncertain retries additionally require confirmNoDelivery=true or return 409. Unknown jobs/actions return 400.
- OpenWA callbacks require the active transport session, but persisted account identity survives session rotation.

## Existing Compose deployment

The current Compose project runs the Bun bridge and a local Telegram API server. Persistent named volumes retain bridge data, media and Telegram API data. The bridge reads Telegram API data read-only and connects to a separately configured external OpenWA network. Its HTTP port binds host loopback. Private environment files use raw value handling to preserve literal credentials.

The Go target adds PostgreSQL as the durable runtime store and keeps Compose as the setup workflow. Actual service definitions, connection variables, PostgreSQL version and migration commands belong to the later implementation phase.
