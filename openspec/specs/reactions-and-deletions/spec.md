# reactions-and-deletions

## Purpose

Synchronize reactions and source revocations while preserving owner deletion choices.

Current TypeScript baseline at `checkpoint-go-contract-baseline`; target changes belong in change specs.

## Requirements

### Requirement: Native reactions

WhatsApp reactions SHALL use the available actor member bot, with attributed master fallbacks where unavailable or unsupported, and clear only the departing actor reaction.

#### Scenario: Reaction snapshot

- **WHEN** a full source snapshot omits a previous reactor
- **THEN** that actor reaction is removed while remaining actors are preserved

### Requirement: Reaction mapping

Reactions SHALL target retained mapped copies with private bot scope, prefer fresh copies and attachments for split deliveries, and tolerate missing messages or topics.

#### Scenario: Target removed

- **WHEN** a mapped Telegram reaction target no longer exists
- **THEN** later conversation messages are not blocked

### Requirement: Owner reactions

Owner Telegram reactions SHALL forward supported Unicode or resolved custom-emoji equivalents, suppress source echoes and deduplicate equivalent changes; unresolved custom emoji SHALL not clear the existing source reaction.

#### Scenario: Custom emoji unresolved

- **WHEN** the provider cannot resolve a newly selected custom emoji
- **THEN** a temporary independent notice appears and the prior WhatsApp reaction remains

### Requirement: Reaction retries

Reaction updates and removals SHALL retry safely without becoming uncertain message sends; prepared custom-emoji payloads SHALL stay frozen across known retries.

#### Scenario: Native update timeout

- **WHEN** setting a Telegram reaction times out
- **THEN** the reaction operation may retry without pausing ordinary message delivery as an uncertain send

### Requirement: Other-author revocation

Revocations of other authors SHALL remove retained mapped text/media copies automatically without destructive replacement text; out-of-order revocations SHALL prevent history resurrection.

#### Scenario: Revocation arrives first

- **WHEN** a revoke event precedes its message delivery
- **THEN** later live or history delivery does not resurrect the message

### Requirement: Owner revocation choice

Revoked owner messages SHALL remain until a scoped Delete/Keep choice; Keep and expiry SHALL preserve content, and Delete SHALL remove all parts only in the selected destination.

#### Scenario: Choice expires

- **WHEN** the owner does not answer before expiry
- **THEN** buttons or fallback prompts expire while the original content remains

### Requirement: Revocation authorization

Deletion choices SHALL validate owner, bot, message, destination, session and expiry, and completed choices SHALL not be reversed by repeated events or clicks.

#### Scenario: Conflicting repeated click

- **WHEN** Keep was already completed before a stale Delete click
- **THEN** the original remains preserved

### Requirement: Explicit source actions

/delete and /react SHALL require a mapped reply and respect provider permissions; deletion refusal SHALL preserve remaining content.

#### Scenario: Provider denies deletion

- **WHEN** a mapped deletion is refused
- **THEN** the bridge does not overwrite the surviving Telegram message with destructive replacement content

## Evidence

Source: [src/reactions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/reactions.ts), [src/revocations.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/revocations.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts).

Regression fixtures: [reactions.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts), [revocations.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts). See the [traceability index](../../traceability.md) for named cases and verification limits.
