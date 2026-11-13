# owner-controls

## Purpose

Provide scoped owner commands and durable cleanup without expiring conversation archives.

Current TypeScript baseline at `checkpoint-go-contract-baseline`; target changes belong in change specs.

## Requirements

### Requirement: Command addressing

Unqualified group commands SHALL belong to the master; member bots SHALL handle commands explicitly addressed to themselves; commands for other bots and Telegram service events SHALL not relay.

#### Scenario: Edited command

- **WHEN** a command or former command is edited
- **THEN** it is neither executed again nor sent to WhatsApp

### Requirement: Conversation preferences

/mute and /ignore SHALL suppress new relays until /keep; creating a bot at capacity SHALL preserve master routing.

#### Scenario: Resume conversation

- **WHEN** the owner selects /keep for a muted conversation
- **THEN** future relays resume

### Requirement: Scoped callbacks

Interactive controls SHALL validate their owner, bot, conversation, topic, identity and expiry before changing state.

#### Scenario: Reissue an expired contact-binding prompt

- **WHEN** an authenticated management action requests `binding_choices` for an unbound registered bot whose current credential has completed native setup
- **THEN** a new owner-private prompt freezes fresh scoped choices and a separate delivery key, preserving old choice expiry, credentials and all bindings; retries reuse the new prompt and a bound or unready bot is refused

#### Scenario: Wrong topic callback

- **WHEN** a valid control is replayed from another topic
- **THEN** it cannot change the original conversation

### Requirement: Durable notice expiry

Commands and notices SHALL expire after 60 seconds by default and interactive controls after 600 seconds, surviving restart; WhatsApp chat messages and attachments SHALL not expire with them.

#### Scenario: Restart before expiry

- **WHEN** the bridge restarts before notice expiry
- **THEN** cleanup still removes the notice when due

### Requirement: Clear notices

/clear log, /clear_log and /clear SHALL remove recorded local notices and commands while preserving conversation messages, bindings and mappings.

#### Scenario: One deletion refused

- **WHEN** Telegram refuses one recorded notice deletion
- **THEN** other eligible notices are still removed

### Requirement: Clear management archive

/clear all and /clear_all SHALL remove only recorded messages in the invoking management topic, including General or Status, and report protected or refused deletions; mirrors and private chats SHALL support notices only.

#### Scenario: Clear General

- **WHEN** the owner clears all recorded General messages
- **THEN** other topics, later messages and delivery mappings remain intact

### Requirement: Health and menus

Startup SHALL register commands for migrated master and member bots, and /status SHALL report configuration, queue state, counts, sync and cleanup information.

#### Scenario: Existing recorded history

- **WHEN** an upgrade indexes prior archived messages for manual clearing
- **THEN** those messages are not automatically expired

## Evidence

Source: [src/control.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/control.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts).

Regression fixtures: [control.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts), [group-picker.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/group-picker.test.ts). See the [traceability index](../../traceability.md) for named cases and verification limits.
