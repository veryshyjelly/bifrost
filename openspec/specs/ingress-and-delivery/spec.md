# ingress-and-delivery

## Purpose

Authenticate inputs and retain durable, ordered delivery with explicit review of ambiguous sends.

Current TypeScript baseline at `checkpoint-go-contract-baseline`; target changes belong in change specs.

## Requirements

### Requirement: HTTP surface

The bridge SHALL expose GET /healthz, authenticated GET /v1/status, and POST /webhooks/openwa, /webhooks/telegram/:bot, /v1/management and /v1/review, returning JSON and rejecting unsupported routes.

#### Scenario: Health request

- **WHEN** GET /healthz is called without admin authorization
- **THEN** the response reports status and configured state without exposing secrets

### Requirement: Ingress authentication

OpenWA ingress SHALL verify HMAC-SHA256 over raw request bytes; Telegram SHALL verify the bot-specific secret header; management SHALL verify signed content and a seven-day timestamp window; status/review SHALL require an admin bearer token.

#### Scenario: Forged payload

- **WHEN** signed OpenWA bytes are altered before delivery
- **THEN** the request is rejected before acceptance

### Requirement: Payload validation

Ingress SHALL reject bodies above 2 MiB, invalid JSON and non-object JSON; OpenWA SHALL require a supported event, current session, idempotency key and data.

#### Scenario: Oversized webhook

- **WHEN** a webhook body exceeds 2 MiB
- **THEN** the bridge responds 413 without accepting it

### Requirement: Owner admission

Telegram SHALL admit only the configured non-bot owner with a valid numeric update ID for known bots, and ignore unauthorized actors.

#### Scenario: Other user update

- **WHEN** another user sends an ordinary message to a bridge bot
- **THEN** no WhatsApp send job is accepted

### Requirement: Durable deduplication

Accepted events SHALL deduplicate durably by source keys and preserve ordering within a stream; failed or uncertain earlier jobs SHALL prevent later work in that stream from overtaking.

#### Scenario: Repeated event

- **WHEN** an accepted source event is delivered again after restart
- **THEN** no duplicate job or completed external effect is created

### Requirement: Stable external effects

Completed external effects SHALL be immutable; outgoing WhatsApp sends SHALL retain the same idempotency key across retries and private bot IDs SHALL isolate overlapping Telegram message IDs.

#### Scenario: Partial multipart send

- **WHEN** a first part completed before a later part failed
- **THEN** retry sends only incomplete parts

### Requirement: Known failures versus ambiguity

Known rate refusals SHALL honor retry delay; mutations with ambiguous network, malformed response or server-error outcomes SHALL require review rather than automatic replay; known pre-connection failure SHALL remain retryable.

#### Scenario: Send timeout

- **WHEN** the request may have reached the provider before timing out
- **THEN** the job becomes uncertain and later same-stream sends wait

### Requirement: Restart recovery

Explicit startup recovery SHALL restore running jobs to pending and sending effects to uncertain; opening a second Store SHALL not reset a live worker.

#### Scenario: Crash during mutation

- **WHEN** the process stops while an effect is marked sending
- **THEN** restart preserves the effect as uncertain for review

### Requirement: Explicit review

POST /v1/review SHALL accept retry or skip for existing failed/uncertain jobs; retrying an uncertain job SHALL require confirmNoDelivery=true and preserve completed effects and original send keys.

#### Scenario: Unconfirmed retry

- **WHEN** an uncertain job is retried without explicit confirmation
- **THEN** the endpoint returns 409 and retains the uncertainty

### Requirement: Safe error reporting

Transport errors SHALL expose safe status/reason codes without tokens or provider descriptions containing private data.

#### Scenario: Provider embeds secret

- **WHEN** an error description contains private provider data
- **THEN** logs and returned safe errors omit that description

## Evidence

Source: [src/server.ts](../../../src/server.ts), [src/api.ts](../../../src/api.ts), [src/store.ts](../../../src/store.ts), [src/bridge.ts](../../../src/bridge.ts).

Regression fixtures: [http.test.ts](../../../test/http.test.ts), [bridge.test.ts](../../../test/bridge.test.ts), [identity.test.ts](../../../test/identity.test.ts). See the [traceability index](../../traceability.md) for named cases and verification limits.
