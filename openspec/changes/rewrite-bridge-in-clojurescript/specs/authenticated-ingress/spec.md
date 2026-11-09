# Spec Delta

## Purpose

Authenticate incoming requests and admit durable work only within the existing account, bot and owner boundaries.

## ADDED Requirements

### Requirement: Exact request authentication and size limits
Ingress SHALL preserve raw-body OpenWA signature verification, bot-specific Telegram secrets, signed management actions and the 2 MiB body cap. Status/review administration MUST require its existing authentication; malformed bodies and unknown routes MUST retain their refusal behavior.

#### Scenario: Signature over different bytes
- **WHEN** a caller signs normalized JSON but sends different raw bytes
- **THEN** OpenWA authentication fails without enqueuing the event.

#### Scenario: Oversized request
- **WHEN** a request body exceeds the cap, including through chunked delivery
- **THEN** ingress rejects it and does not parse or enqueue it as a valid event.

#### Scenario: Scoped Telegram or management secret
- **WHEN** a secret belongs to another bot, or a management action has an invalid signature or invalid age under the existing contract
- **THEN** admission fails without changing delivery state.

### Requirement: Compatible event admission and deduplication
OpenWA admission SHALL validate the configured transport session and supported event contract, preserve idempotency/stream keys, and stamp stable account identity. Telegram admission MUST retain safe update IDs and existing accepted/ignored event behavior.

#### Scenario: Duplicate provider update
- **WHEN** a previously admitted event arrives again
- **THEN** its durable deduplication key prevents a second queued delivery.

#### Scenario: Admitted but unhandled update
- **WHEN** an existing admitted update type has no forwarding branch, such as `poll_answer`
- **THEN** the rewrite preserves that behavior and does not invent an external send.

### Requirement: Owner authorization independent of observation
The bridge SHALL authorize execution using the existing non-bot owner checks and member-bot group filtering. Cleanup observation in recognized chats MUST NOT imply forwarding authorization.

#### Scenario: Observed non-owner message
- **WHEN** a non-owner message is observed in a recognized destination
- **THEN** permissible ledger observation occurs but the message does not become authorized WhatsApp work.

#### Scenario: Member bot sees an ordinary group message
- **WHEN** a member bot receives an ordinary group update that is neither an admitted control nor explicitly addressed under the existing rules
- **THEN** it is ignored so the manager path cannot overlap with a second send.

### Requirement: Public and administrative status boundaries
Health status SHALL retain its public process/configuration meaning, while detailed status and delivery review MUST retain administrative authorization and sanitized errors.

#### Scenario: Unauthenticated administrative query
- **WHEN** an unauthenticated caller requests detailed status or delivery review
- **THEN** access is refused without leaking credentials, private errors or delivery controls.
