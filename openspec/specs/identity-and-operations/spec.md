# identity-and-operations

## Purpose

Preserve permanent identity and private state across configuration changes, setup and machine transfer.

Current TypeScript baseline at `checkpoint-go-contract-baseline`; target changes belong in change specs.

## Requirements

### Requirement: Permanent account identity

BIFROST_ACCOUNT_ID SHALL remain independent of OpenWA transport credentials, adopt legacy stored identity, and reject conflicts with existing state.

#### Scenario: Session rotates

- **WHEN** OpenWA session and API key change while the same database is retained
- **THEN** receipts, checkpoints, aliases and uncertain sends retain their account identity

### Requirement: Protected configuration

Fresh configuration SHALL generate independent private secrets; reconfiguration SHALL preserve keys and operator choices; stored bot tokens SHALL be encrypted rather than plaintext.

#### Scenario: Reconfigure existing bridge

- **WHEN** configuration is regenerated over an existing private environment
- **THEN** the encryption key and operator configuration remain unchanged

### Requirement: Configuration validation

Startup SHALL validate encryption key format, minimum secret lengths, port, media and history limits, owner/group identifiers, pacing, cron/timezone and notice TTL bounds.

#### Scenario: Invalid TTL

- **WHEN** a configured command lifetime is outside 5 through 86400 seconds
- **THEN** configuration validation refuses startup

### Requirement: Setup readiness

Setup SHALL require a complete connected OpenWA session, migrate Telegram bots before local API calls and install both providers webhooks.

#### Scenario: Disconnected session

- **WHEN** setup sees an incomplete or disconnected session
- **THEN** migration and callback registration do not proceed

### Requirement: Webhook subscriptions

OpenWA webhook setup SHALL add missing bridge events only for the bridge callback, preserving existing settings and wildcard subscriptions.

#### Scenario: Existing wildcard hook

- **WHEN** a callback already subscribes to all events
- **THEN** setup leaves it idempotently intact

### Requirement: Session notices

Session status/disconnection and message.failed events SHALL become management notices rather than ordinary conversation relays.

#### Scenario: Session disconnects

- **WHEN** a supported disconnect event is accepted
- **THEN** a management status action is queued

### Requirement: Transfer preservation

Export/import SHALL preserve account identity, keys, receipts, state and media, normalize only local paths, retain literal credentials and refuse ambiguous or bind-mounted state.

#### Scenario: Ambiguous volumes

- **WHEN** export discovers multiple candidate state volumes
- **THEN** it refuses to choose one implicitly

### Requirement: Archive validation

Transfer archives SHALL reject traversal, duplicate paths and link entries before extraction, preserve ownership, and avoid starting restored bots automatically.

#### Scenario: Malicious archive path

- **WHEN** an import archive contains a parent-directory traversal entry
- **THEN** import refuses extraction

## Evidence

Source: [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/config.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/config.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [scripts/configure.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/configure.ts), [scripts/setup.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/setup.ts), [scripts/transfer.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/transfer.ts).

Regression fixtures: [identity.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/identity.test.ts), [configure.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/configure.test.ts), [setup.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/setup.test.ts), [transfer.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/transfer.test.ts). See the [traceability index](../../traceability.md) for named cases and verification limits.
