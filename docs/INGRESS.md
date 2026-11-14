# Durable event admission

The Go runtime can authenticate and retain OpenWA, Telegram and management events in PostgreSQL. This implements admission, not automatic message routing or a ready live cutover. No provider callback is registered and no polling or outgoing provider operation starts with the HTTP service.

Complete native WhatsApp messages now receive an immutable source snapshot and stable conversation stream in the same admission transaction. Verified phone/LID aliases preserve existing conversation state. See [normalization and dispatcher behavior](DISPATCH.md); the HTTP service still does not launch the worker.

## Enable explicitly

`BRIDGE_INGRESS_ENABLED` defaults to `false`, preserving diagnostic startup for a fresh Compose setup. Enabling it requires a current `OPENWA_SESSION_ID`, a minimum 32-character `OPENWA_WEBHOOK_SECRET`, a positive safe-integer `TELEGRAM_OWNER_ID`, a negative safe-integer `TELEGRAM_MANAGEMENT_CHAT_ID`, and a `TELEGRAM_MASTER_TOKEN` identifying the master bot. Existing account, encryption and admin settings remain required. Errors identify setting names without disclosing values.

Apply embedded migrations with the normal explicit migration step before starting admission. For an isolated development instance, add `BRIDGE_INGRESS_ENABLED=true` to its private `.go.env` after filling provider settings. Do not register the live bridge's callbacks against this partial Go runtime or run a second consumer against its identity. Workflow execution and existing-state import still need implementation and validation.

Health/status JSON retains `configured: false` until the complete bridge exists and separately reports `ingress: true` when admission is installed. With admission disabled, its endpoints return JSON 404 responses.

## HTTP contract

| Endpoint | Authentication | Success |
| --- | --- | --- |
| `POST /webhooks/openwa` | `X-OpenWA-Signature: sha256=<hex>` over the exact raw bytes | `{"accepted":true}` for a new event; false for a duplicate |
| `POST /webhooks/telegram/:bot` | `X-Telegram-Bot-Api-Secret-Token` containing HMAC-SHA256 of the bot key using the admin token | New admitted owner event returns true; duplicate or ignored actor returns false |
| `POST /v1/management` | `signature` field containing HMAC-SHA256 of the unsigned body's JavaScript JSON serialization using the admin token | New signed event returns true; duplicate returns false |

Bodies are limited to 2 MiB and must contain one JSON object. Telegram authentication happens before reading its body. OpenWA authentication checks the original bytes before parsing. Invalid authentication returns 401; invalid payloads return 400; oversized bodies return 413; authenticated unknown bots return 404. Persistence failures return a safe JSON 503, allowing provider retry. An admission operation has a ten-second deadline.

OpenWA signature verification uses the maintained SDK's native helper. Telegram decoding uses `go-telegram/bot/models`, including managed-bot, poll-answer, membership, callback and reaction updates. The custom HTTP acknowledgement is intentional: the SDK's webhook channel/handler pipeline does not provide this application's transaction result as its HTTP response. Returning success before a durable PostgreSQL commit would violate Bifrost's delivery contract.

Management signatures remain compatible with the previous `JSON.stringify` protocol. Property insertion order, integer property order, duplicate-key semantics, number formatting and string escaping matter; sorting a Go map would break existing signatures. Cross-runtime fixtures are generated with Node's JSON and HMAC implementations, and Go tests verify the fixed results. Management timestamps must be within seven days in either direction, and events require an ID and type.

## Admission and ordering

OpenWA accepts the existing ten event types: message received/sent/edited/revoked/reaction/failed, poll vote, Status received, session status and session disconnected. The session and idempotency key are mandatory. The runtime overwrites the envelope's account ID with its permanent identity. Status events use the shared Status stream; other events use the WhatsApp chat or session stream.

Telegram accepts the configured non-bot owner for the master or a registered contact bot. Ordinary group messages received through contact bots are ignored unless they address that bot's username; owner callbacks and bot membership events retain their existing exceptions. Private streams include the bot ID so overlapping private message IDs cannot collide. Known whole-group mirrors ignore incidental reply-thread IDs; forum bindings and unknown real topic messages preserve their effective topic. Retired management inbox bindings do not match a whole-chat route.

Message observations needed by cleanup are distinct from permission to forward. Eligible messages in known chats are observed even when another user or a bot authored them. Group observations share a scope across receiving bots; private observations retain the bot key. Protected/service/log/command/chat kinds and the raw message are retained. Existing chat/command classifications survive duplicate observation. Observation and admitted owner work commit together, so an HTTP success cannot leave only one persisted. Observation-only traffic cannot become a delivery job.

PostgreSQL source keys remain `ow:<idempotencyKey>`, `tg:<bot>:<update_id>` and `management:<id>`, scoped to the permanent account. A duplicate after a process restart returns false and retains the original payload. This composes with the existing stream-head queue and explicit uncertain-send review; ingress itself performs no external sends.

Migration `00002_ingress.sql` adds bot metadata, Telegram-facing conversation bindings and retained message observations. Bot tokens are not stored in these metadata rows. Registration, routing controllers, cleanup execution and the explicit importer will populate/use these records as their workflows are ported.

## Validation

Focused tests cover byte tampering, body bounds, wrong sessions/events, bot-specific secrets, owner filtering, account and private-bot isolation, forum/mirror streams, native update types, management signatures/timestamps, and responses withheld until commit. Real isolated PostgreSQL tests additionally cover concurrent duplicate HTTP deliveries, replay after reopening, atomic observation/queue rollback, protected cleanup classification and retired bindings. The container smoke test verifies authenticated admission and duplicate replay after a real process restart without contacting Telegram or WhatsApp.
