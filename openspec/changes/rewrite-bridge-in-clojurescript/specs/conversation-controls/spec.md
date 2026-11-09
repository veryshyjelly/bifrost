# Spec Delta

## Purpose

Preserve owner-operated conversation management, group transitions and Telegram control cleanup without widening routing or deletion scope.

## ADDED Requirements

### Requirement: Validated group selection and linking
Group picker and manual linking SHALL retain account/owner/request/route validation, expiry and reuse checks. Linking MUST verify the permitted private Telegram group, manager administration and unique destination without automatically changing platform membership.

#### Scenario: Stale picker response
- **WHEN** a group selection belongs to an expired, used or changed route request
- **THEN** the selection cannot overwrite the current conversation destination.

#### Scenario: Successful mirror link
- **WHEN** the owner selects a valid authorized group
- **THEN** the existing route/inbox provenance is persisted, metadata work and retirement are queued independently, and history is not imported automatically.

### Requirement: Durable return to inbox
Unmirror SHALL retain its owner/manager/context checks, history guard, topic-creation receipt and route checkpoint. It MUST preserve mute/ignore settings, archived messages and protection against stale notification retries.

#### Scenario: Topic creation refusal or uncertainty
- **WHEN** returning a mirror encounters a definite permission refusal or an uncertain topic-creation result
- **THEN** the existing mirror is not silently replaced, and uncertainty remains available for explicit review.

### Requirement: Existing command and menu surface
All commands inventoried in document 02 SHALL retain their context restrictions, literal user presentation and owner checks. Addressed commands MUST remain bot-specific, and service messages or edited commands MUST NOT become ordinary sends.

#### Scenario: Command addressed elsewhere
- **WHEN** a command names another bot
- **THEN** it is ignored under the existing addressed-command rules.

#### Scenario: Edit of a former command
- **WHEN** an observed command is later edited into ordinary-looking text
- **THEN** the edit is not forwarded as a new WhatsApp message.

### Requirement: Workflow-specific control validity
Specialized controls SHALL preserve their existing owner, account, bot, chat, topic, expiry, revision and stale-retry rules as applicable. Generic callbacks and keep routing MUST be evaluated under the correctness-exception policy rather than silently acquiring new semantics.

#### Scenario: Stale specialized control
- **WHEN** a repeated or expired specialized action conflicts with a newer decision
- **THEN** it cannot undo that decision or operate in another conversation.

### Requirement: Scoped expiring notices and clearing
Commands/notices SHALL retain configured TTLs while ordinary chat copies remain unexpired. Clear operations MUST retain bot-private/group scope, topic and sequence cutoffs, protected/old-message rules, refusal fallback and independent deletion retries, preserving bindings and source mappings.

#### Scenario: Clear log or clear all
- **WHEN** an authorized clear operation runs
- **THEN** it affects only its allowed recorded scope and cutoff, leaving other topics, bots, later messages and durable mappings intact.

#### Scenario: Partial cleanup failure
- **WHEN** deletion is partly refused or its outcome is ambiguous
- **THEN** remaining cleanup follows its idempotent recovery contract without replaying accepted chat sends.
