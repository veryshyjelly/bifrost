# history-import

## Purpose

Import manually selected history with durable checkpoints, native replies and destination-scoped receipts.

Current TypeScript baseline at `checkpoint-go-contract-baseline`; target changes belong in change specs.

## Requirements

### Requirement: Explicit history choice

/history [n] SHALL offer Skip existing, Import all and Cancel, defaulting to 20 and accepting explicit counts 1 through 2000; no fetch or live-delivery hold SHALL start while awaiting choice.

#### Scenario: Cancelled import

- **WHEN** the owner selects Cancel
- **THEN** no history fetch starts and live messages continue

### Requirement: Choice authorization

A history choice SHALL be single-use, expiring and scoped to owner, bot, account/session identity, destination, topic and binding.

#### Scenario: Double click

- **WHEN** a completed choice is clicked again
- **THEN** no second import starts and the import mode is unchanged

### Requirement: Complete ordered batch

The bridge SHALL fetch the complete available batch before posting oldest first, order parents before children, and use deep-history fetching above 100.

#### Scenario: Fetch fails

- **WHEN** history retrieval fails before posting
- **THEN** live delivery is released and no topic is deleted

### Requirement: Destination receipts

Skip existing SHALL skip delivered copies in that destination; Import all SHALL create fresh copies, exclude revoked messages and preserve other destinations.

#### Scenario: Manually cleared Telegram history

- **WHEN** the owner chooses Import all while old receipts remain
- **THEN** new copies are created even if duplicates may still exist

### Requirement: History quotes

Replies within an Import all batch SHALL target the new copies; out-of-window quotes SHALL use previews rather than possibly cleared old copies.

#### Scenario: Quote outside import

- **WHEN** a message quotes a source absent from the batch
- **THEN** a short quote preview is shown

### Requirement: Restartable import

A partial import SHALL resume its frozen batch, receipts, media and presentation without duplicating completed parts, even after OpenWA credentials rotate; only that conversation live stream SHALL be held during active import.

#### Scenario: Worker restarts

- **WHEN** an import stops after an attachment is delivered
- **THEN** restart resumes remaining parts using saved receipts

### Requirement: History presentation

New imports SHALL retain daily date headings and compact native-time messages, use the configured timezone for day boundaries, identify owner messages as You, and omit empty sections for skipped messages.

#### Scenario: Midnight boundary

- **WHEN** delivered history crosses a local day boundary
- **THEN** one retained date heading starts each day

### Requirement: Prompt cleanup

Selecting a history choice SHALL delete only its prompt; cleanup failure SHALL retry independently without blocking the import.

#### Scenario: Prompt deletion times out

- **WHEN** an import choice is accepted but Telegram prompt deletion fails
- **THEN** the import can continue while cleanup retries

## Evidence

Source: [src/history.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/history.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts).

Regression fixtures: [history.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts), [identity.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/identity.test.ts). See the [traceability index](../../traceability.md) for named cases and verification limits.
