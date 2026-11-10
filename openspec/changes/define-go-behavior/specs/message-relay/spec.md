# Message relay changes

## MODIFIED Requirements

### Requirement: Telegram edits

Owner text and nonempty caption edits SHALL update the mapped WhatsApp message; definitive refusal SHALL produce a temporary notice and permit later sends. Clearing a mapped outgoing media caption SHALL be supported when the latest released provider API permits it, sending an explicitly empty caption through the required SDK. When clearing is unsupported or definitively refused, the existing WhatsApp caption SHALL remain and the owner SHALL receive a temporary explanation. A caption-clear edit SHALL NOT send a new message, delete the media or clear an unrelated text message. Known retries and uncertain outcomes SHALL retain the existing delivery safety rules.

#### Scenario: Refused edit

- **WHEN** WhatsApp refuses an owner edit
- **THEN** the owner sees that it was not applied and the next message proceeds

#### Scenario: Clear a supported caption

- **WHEN** the owner removes the caption from a mapped outgoing media message in Telegram and the latest released provider API supports clearing that caption
- **THEN** the bridge sends an explicit empty-caption edit for the existing WhatsApp message
- **AND** the media and existing message mappings remain intact without creating a new message

#### Scenario: Caption clearing unsupported

- **WHEN** the owner removes a mapped outgoing caption but the selected provider does not support clearing it
- **THEN** the existing WhatsApp caption remains and a temporary notice explains that clearing was not applied
- **AND** later conversation sends continue

#### Scenario: Retry a rate-limited clear

- **WHEN** a prepared empty-caption edit receives a known rate-limit refusal
- **THEN** retry honors the provider delay and reuses the original target, explicit empty caption, mention-removal payload and stable edit effect identity

#### Scenario: Ambiguous clear outcome

- **WHEN** an empty-caption edit may have reached WhatsApp before a transport failure
- **THEN** the effect remains uncertain and requires explicit review under the existing delivery contract
- **AND** the bridge does not claim that the caption was preserved or cleared without confirmation
