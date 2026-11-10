# status-feed

## Purpose

Archive WhatsApp Status posts in one management topic with independent author mute preferences.

Current TypeScript baseline at `checkpoint-go-contract-baseline`; target changes belong in change specs.

## Requirements

### Requirement: Shared Status archive

Startup and /statuses SHALL refresh available posts into one WhatsApp Status topic, deduplicating webhook races without creating conversations.

#### Scenario: Source expires

- **WHEN** an already archived Status expires on WhatsApp
- **THEN** its Telegram copy remains

### Requirement: Status content

Text, image, video and voice posts SHALL show linked author and native posting time; authenticated local retrieval SHALL preserve captions when media is missing.

#### Scenario: Untrusted media URL

- **WHEN** a Status advertises an external media URL
- **THEN** the bridge uses its authenticated local Status endpoint

### Requirement: Passive Status feed

Reading or refreshing the archive SHALL send no WhatsApp view/read receipt; topic text, edits, replies and reactions SHALL not publish Status posts or send WhatsApp messages.

#### Scenario: Owner replies in feed

- **WHEN** the owner sends ordinary text in the Status topic
- **THEN** no WhatsApp message or Status publication is sent

### Requirement: Independent Status mutes

Mapped Status reply commands and paginated unmute controls SHALL persist author preferences across aliases and restarts, without muting chats or deleting archives.

#### Scenario: Unmute later

- **WHEN** an author was muted while new posts arrived and is then unmuted
- **THEN** future posts resume but skipped posts are not replayed

### Requirement: Status control ordering

Scoped mute controls SHALL enforce expiry and reject stale actions that would reverse a newer preference.

#### Scenario: Old unmute callback

- **WHEN** an unmute callback predates a later mute
- **THEN** the later mute remains effective

## Evidence

Source: [src/statuses.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/statuses.ts), [src/openwa.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/openwa.ts).

Regression fixtures: [statuses.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts). See the [traceability index](../../traceability.md) for named cases and verification limits.
