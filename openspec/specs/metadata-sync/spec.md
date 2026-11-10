# metadata-sync

## Purpose

Refresh Telegram metadata from WhatsApp without writing to WhatsApp or blocking chat work.

Current TypeScript baseline at `checkpoint-go-contract-baseline`; target changes belong in change specs.

## Requirements

### Requirement: Scoped metadata refresh

/sync SHALL refresh the current conversation or bound bot contact; /sync all SHALL require the master private or management context.

#### Scenario: Bound group-only contact

- **WHEN** the owner uses /sync in its bot DM before a direct route exists
- **THEN** the contact is refreshed without creating a global sync

### Requirement: Metadata preservation

Refresh SHALL update available group/contact metadata and existing bots, preserve unavailable pictures/descriptions and incomplete rosters, and never overwrite the shared management group for an inbox route.

#### Scenario: Group lookup fails

- **WHEN** metadata lookup fails while a contact bot remains known
- **THEN** the stored roster is preserved and other contact refreshes may continue

### Requirement: Efficient metadata work

Content-hash picture caching SHALL avoid duplicate uploads, shared contact bots SHALL be fetched once per run, and chat jobs SHALL have priority over metadata work.

#### Scenario: Rotating signed picture URL

- **WHEN** the URL changes while picture bytes remain identical
- **THEN** no duplicate Telegram upload occurs

### Requirement: No source mutations

Metadata refresh SHALL perform no WhatsApp message, history, read, membership or profile writes and SHALL offer scoped permission guidance for missing Telegram rights.

#### Scenario: Permission missing

- **WHEN** a mirror cannot update metadata due to permissions
- **THEN** a scoped grant action is offered without a WhatsApp mutation

### Requirement: Persistent scheduling

Cron SHALL use the configured timezone, default hourly in Asia/Kolkata, persist one run per scheduled minute across restarts, support disabled schedules and reject malformed expressions.

#### Scenario: Restart in same minute

- **WHEN** a scheduled run was already persisted before restart
- **THEN** the same minute is not scheduled twice

## Evidence

Source: [src/sync.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/sync.ts), [src/cron.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/cron.ts).

Regression fixtures: [sync.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts). See the [traceability index](../../traceability.md) for named cases and verification limits.
