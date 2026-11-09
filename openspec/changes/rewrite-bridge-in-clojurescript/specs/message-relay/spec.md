# Spec Delta

## Purpose

Relay intended WhatsApp and Telegram contents with correct identity, destination, presentation and durable source-copy relationships.

## ADDED Requirements

### Requirement: Scoped destination selection
Routing SHALL preserve manager inbox, dedicated private bot and mirrored-group behavior. Dedicated delivery requires readiness; group author preference requires explicit provider membership checks. Own messages, muted/ignored routes and fallback MUST retain their existing rules.

#### Scenario: Ready private contact or absent group bot
- **WHEN** a contact's dedicated bot is ready privately, or the bot is absent from a mirrored group
- **THEN** the existing readiness/membership rule selects the correct bot and destination instead of inferring presence from a binding.

#### Scenario: Whole group and exact topic
- **WHEN** Telegram supplies an incidental reply-thread ID to a whole-group route or a mismatched thread to a configured topic route
- **THEN** the whole-group route remains usable and the mismatched topic does not capture the conversation.

### Requirement: Frozen retries and controlled repair
Delivery targets, presentation, resolved mentions, packet boundaries and frozen media bytes SHALL remain stable during partial retry. Manager fallback and missing-topic repair MUST obey existing preconditions and retain all accepted or uncertain effects.

#### Scenario: Binding or route changes during retry
- **WHEN** a later packet retries after contact, bot or route changes
- **THEN** accepted packets are reused and remaining delivery follows the saved target and presentation.

#### Scenario: Missing topic after partial acceptance
- **WHEN** a missing-topic refusal occurs after any part was accepted or left uncertain
- **THEN** the bridge does not recreate or redirect the delivery as a fresh untracked send.

### Requirement: Quotes and copy provenance
Native replies SHALL use surviving copies in the correct destination/thread and private bot scope. Telegram replies mapped to a different WhatsApp conversation MUST be refused. Every delivered part MUST retain its source mapping and completion provenance.

#### Scenario: Cross-conversation reply
- **WHEN** an outgoing reply points to a mapped copy from another WhatsApp conversation
- **THEN** no send occurs and the scope violation is reported safely.

#### Scenario: Deleted or wrong-bot quote
- **WHEN** an incoming quote has only a deleted, wrong-thread or differently scoped private copy
- **THEN** that copy is not selected as a native reply target.

### Requirement: Literal content and verified mentions
Text, sender attribution, historical styles and captions SHALL preserve literal contents, UTF-16 entity offsets and atomic date spans. Mention conversion MUST use validated identity metadata, aliases and roster checks, retaining the existing refusal and cached/frozen rendering behavior.

#### Scenario: Astral characters across message limits
- **WHEN** a formatted message splits near an astral character or atomic time entity
- **THEN** the contents reconstruct exactly and entity offsets remain valid without splitting atomic spans.

#### Scenario: Unknown or departed mention
- **WHEN** a copied tag is invalid or its resolved person is not a current validated member
- **THEN** a scoped notice is delivered without sending the tagged message, and later independent text can continue.

### Requirement: Provider transport and outcome classification
Telegram JSON/multipart and OpenWA paths, headers, request bodies and stable idempotency headers SHALL remain compatible. Definite refusal, rate limiting, read failure and ambiguous mutation MUST retain distinct safe classifications; missing outgoing message IDs MUST remain uncertain.

#### Scenario: Acceptance followed by timeout or missing ID
- **WHEN** the bridge cannot establish a mutation receipt after possible acceptance
- **THEN** the outcome remains blocked for review rather than being automatically resent.

#### Scenario: Read receipt fails after a send
- **WHEN** marking a conversation read fails after a valid outgoing receipt and mapping
- **THEN** separate read work retries or fails without replaying the send.

### Requirement: Authenticated bounded media and structured contents
Each existing media and structured-content class SHALL retain its contents, mapping, conversion and fallback behavior. Downloads MUST use authenticated allowed endpoints, enforce streaming caps and realpath containment, and preserve private files, codec isolation and byte-stable retries.

#### Scenario: Unavailable or unsupported native attachment
- **WHEN** an attachment is missing/oversized or its native presentation is definitively refused under existing fallback rules
- **THEN** the existing unavailable notice or original-document fallback preserves text and prior receipts.

#### Scenario: Unsafe path or changed frozen sticker
- **WHEN** a local media path escapes its allowed root or regenerated sticker bytes differ from the saved retry snapshot
- **THEN** the operation is refused without forwarding alternate private bytes or silently changing the frozen send.
