# conversation-routing

## Purpose

Route owner conversations between WhatsApp, management topics, private member bots and linked groups.

Current TypeScript baseline at `checkpoint-go-contract-baseline`; target changes belong in change specs.

## Requirements

### Requirement: Inbox creation

An unknown eligible conversation SHALL create one management topic, deduplicate replay, and remain unread on WhatsApp.

#### Scenario: First incoming message

- **WHEN** an unknown contact sends a message
- **THEN** one topic and one relay are recorded without a read receipt

### Requirement: Member bot identity

The bridge SHALL reuse a contact bot across known phone/LID aliases and groups, selecting it only where available; otherwise the master SHALL relay with attribution.

#### Scenario: Bot absent from group

- **WHEN** a bound bot is absent from a mirrored group
- **THEN** the master relays the message with the sender name

### Requirement: Private route activation

A bound member bot SHALL require Start for incoming private routing and SHALL permit its owner DM to create a missing direct route for a group-only contact.

#### Scenario: First private reply

- **WHEN** the owner messages a started bound bot whose contact has only appeared in a group
- **THEN** a direct route is created and the message is sent to that contact

### Requirement: Group linking

The bridge SHALL permit owner-authorized private group selection or manual /link, reject public and management destinations, and validate picker expiry and session.

#### Scenario: Unauthorized selection

- **WHEN** another user shares a group for a pending picker
- **THEN** no route is linked

### Requirement: Inbox retirement

Linking a mirror or starting a bound private bot SHALL retire the former inbox independently of delivery, without automatic history copying or deletion of shared topics.

#### Scenario: Cleanup refused

- **WHEN** Telegram denies deletion of the old topic
- **THEN** conversation delivery continues and cleanup remains separately retryable

### Requirement: Return to management

/unmirror SHALL durably create a new management topic, preserve former mirror messages and preferences, and stop forwarding from the old mirror; an active import SHALL prevent the move.

#### Scenario: Creation refused

- **WHEN** topic creation is definitively refused
- **THEN** the existing mirror remains active and the owner receives retry guidance

### Requirement: Route recovery

A definitively missing topic or blocked member bot SHALL allow route repair before delivery; completed effects SHALL retain their original destination.

#### Scenario: Partial delivery

- **WHEN** routing changes after one part was delivered
- **THEN** the completed part is not resent into the new route

### Requirement: Managed bot registration and binding

The bridge SHALL accept owner-created managed bot updates, retrieve and encrypt the bot token, install its webhook, and require binding to a pending contact request. One bot SHALL NOT bind to two different people.

#### Scenario: Bot already bound

- **WHEN** a binding action selects a bot already assigned to another contact
- **THEN** the bridge rejects reassignment rather than merging the contacts

## Evidence

Source: [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts).

Regression fixtures: [bridge.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts), [group-picker.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/group-picker.test.ts), [unmirror.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/unmirror.test.ts). See the [traceability index](../../traceability.md) for named cases and verification limits.
