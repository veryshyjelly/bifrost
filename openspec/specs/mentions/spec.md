# mentions

## Purpose

Convert explicit tagged identities while preserving validation, identity and rendering across retries.

Current TypeScript baseline at `checkpoint-go-contract-baseline`; target changes belong in change specs.

## Requirements

### Requirement: Incoming mention rendering

Explicit WhatsApp tags SHALL resolve through metadata and aliases to native present-bot mentions, absent-bot profile links or WhatsApp-linked names. Literal numbers SHALL remain literal.

#### Scenario: Identifier placeholder

- **WHEN** a device-specific placeholder and a named cached contact identify the same tagged person
- **THEN** the readable cached identity is used

### Requirement: Outgoing mentions

Registered bot usernames, name entities and copied group tokens SHALL resolve to exact WhatsApp identities in text, captions and edits; code, URL and email entities SHALL be excluded.

#### Scenario: Unrelated username

- **WHEN** ordinary text contains an unregistered username
- **THEN** it remains literal text

### Requirement: Mention picker

/mention SHALL search and paginate the group roster and copy stable identity tokens without sending or marking read.

#### Scenario: Rename after copying

- **WHEN** a copied token name changes before use
- **THEN** the token still identifies the same member

### Requirement: Roster validation

Invalid, wrong-group, other-account or departed-member tokens SHALL be refused before sending; a roster older than five minutes SHALL refresh before a validated send.

#### Scenario: Refresh unavailable

- **WHEN** a stale roster cannot refresh
- **THEN** the picker may show labeled cached results but an unvalidated send is refused

### Requirement: Retry stability

Resolved outgoing mention text and identities and incoming rendering SHALL remain frozen across known retries.

#### Scenario: Binding changes during rate limit

- **WHEN** a bot binding changes after a prepared send receives 429
- **THEN** retry retains the original tags and delivery key

## Evidence

Source: [src/mentions.ts](../../../src/mentions.ts), [src/identity.ts](../../../src/identity.ts), [src/format.ts](../../../src/format.ts).

Regression fixtures: [mentions.test.ts](../../../test/mentions.test.ts). See the [traceability index](../../traceability.md) for named cases and verification limits.
