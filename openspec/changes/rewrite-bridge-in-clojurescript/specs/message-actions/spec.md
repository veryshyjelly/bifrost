# Spec Delta

## Purpose

Reconcile changes to existing message copies while retaining delivery provenance, authorization and the intended archive behavior.

## ADDED Requirements

### Requirement: Scoped edits and frozen presentation
Edits SHALL preserve mapped direction, destination scope, latest-edit storage, mention updates, saved presentation and outgoing echo suppression. Missing/deleted copies MUST remain absent. Split-message edit behavior MUST follow oracle evidence or a verified registered correction.

#### Scenario: Edit arrives before initial delivery
- **WHEN** a saved edit precedes the original message's delivery
- **THEN** the intended latest contents are used without losing source identity or producing an extra original send.

#### Scenario: Definitively refused outgoing edit
- **WHEN** an edit is empty or receives an existing definite nonretryable refusal
- **THEN** its refusal is recorded and a separate notice is queued so later messages can continue; rate-limited or ambiguous edits retain conservative recovery.

### Requirement: Actor-aware reaction reconciliation
Reactions SHALL preserve source actors/emoji, snapshot reconciliation, destination-copy selection, owner echo suppression, native/fallback presentation and validated custom-emoji resolution. An unresolvable outgoing emoji MUST NOT silently clear a prior reaction.

#### Scenario: Unavailable native reaction bot
- **WHEN** the preferred native reaction path is absent or definitively refused
- **THEN** existing manager or attributed-text fallback preserves the source reaction and copy scope.

#### Scenario: Unresolvable custom emoji
- **WHEN** a Telegram custom emoji has no validated standard equivalent
- **THEN** the owner receives the existing separate notice and the prior WhatsApp reaction is retained.

### Requirement: Tombstones and own-revocation choices
Revocation SHALL persist a tombstone before copy reconciliation. Other people's deleted messages and the owner's Keep/Delete choice MUST retain separate durable cleanup behavior, expiry, progress and mappings.

#### Scenario: Late original after revocation
- **WHEN** an original message or history item arrives after its revocation
- **THEN** the tombstone prevents its resurrection.

#### Scenario: Keep or expired own-message choice
- **WHEN** the owner chooses Keep or the choice expires
- **THEN** archived chat contents remain and only the applicable control/prompt is removed.

### Requirement: Existing poll interactions
WhatsApp polls SHALL retain their existing text/buttons, option limits and selection toggling, and authorized votes MUST use the existing vote operation. The rewrite MUST NOT invent forwarding for admitted but unhandled Telegram poll-answer updates.

#### Scenario: Multiple-choice toggle
- **WHEN** the owner repeats a permitted multiple-choice option action
- **THEN** the saved selection toggles under the existing account/conversation/message scope and preserves effect recovery.
