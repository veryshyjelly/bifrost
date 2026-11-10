# message-relay

## Purpose

Preserve message meaning, identity, quotes and edits across the bridge.

Current TypeScript baseline at `checkpoint-go-contract-baseline`; target changes belong in change specs.

## Requirements

### Requirement: Owner sends and echo suppression

Only authorized owner messages SHALL send to WhatsApp; the master SHALL consume ordinary group updates once and suppress send echoes.

#### Scenario: Duplicate group observation

- **WHEN** a tagged member bot and the master receive the same owner group message
- **THEN** only one WhatsApp send occurs

### Requirement: Quote isolation

Replies SHALL use mapped source messages only within the conversation, with private Telegram IDs scoped by bot and exact topic matching in management.

#### Scenario: Foreign quote

- **WHEN** a reply references a message mapped to another WhatsApp conversation
- **THEN** that message is not used as the WhatsApp quote

### Requirement: Sender presentation

Master relays SHALL show a linked sender name and body on one line; member bot relays SHALL omit redundant names; live phone-originated messages SHALL retain owner attribution.

#### Scenario: Unknown phone

- **WHEN** a master relay has no verified sender phone
- **THEN** its name links to the generic WhatsApp launch page

### Requirement: Safe formatting

Untrusted text SHALL remain literal; native Telegram entities SHALL preserve UTF-16 offsets through splitting, captions and edits.

#### Scenario: Markup in source

- **WHEN** a source body contains HTML-like text
- **THEN** it cannot inject formatting or links

### Requirement: Telegram edits

Owner text and nonempty caption edits SHALL update the mapped WhatsApp message; definitive refusal SHALL produce a temporary notice and permit later sends. Clearing a caption SHALL remain unsupported in the baseline.

#### Scenario: Refused edit

- **WHEN** WhatsApp refuses an owner edit
- **THEN** the owner sees that it was not applied and the next message proceeds

### Requirement: WhatsApp edits

WhatsApp edits SHALL update retained mapped Telegram copies with their sender presentation and mentions, without recreating explicitly cleared copies.

#### Scenario: Cleared destination

- **WHEN** an edited source message has a Telegram copy explicitly cleared by the bridge
- **THEN** that copy is not recreated

### Requirement: Structured messages

Locations, contacts and polls SHALL use structured representations, poll votes SHALL produce mapped updates, and missing media SHALL remain visible.

#### Scenario: Poll vote

- **WHEN** a mapped WhatsApp poll receives a vote event
- **THEN** the selected options or cleared vote are represented in Telegram

### Requirement: Read receipts

Successful outgoing replies SHALL schedule a read receipt; /read SHALL explicitly mark the conversation read; receiving relays or fetching history SHALL not.

#### Scenario: Passive archive

- **WHEN** an incoming message is relayed to Telegram
- **THEN** no WhatsApp read operation is sent

### Requirement: Poll choice controls

Mapped poll controls SHALL forward owner choices to the source poll; multiple-choice polls SHALL toggle selections and single-choice polls SHALL replace the previous selection.

#### Scenario: Toggle existing choice

- **WHEN** the owner selects an already selected option on a multiple-choice source poll
- **THEN** that option is removed from the saved selection and the updated choices are sent to WhatsApp

## Evidence

Source: [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts).

Regression fixtures: [bridge.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts), [format.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts), [http.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/http.test.ts). See the [traceability index](../../traceability.md) for named cases and verification limits.
