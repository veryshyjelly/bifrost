# Spec Delta

## Purpose

Import historical messages into an explicitly selected destination while preserving ordering, copy scope and resumable progress.

## ADDED Requirements

### Requirement: Explicit scoped history choice
History requests SHALL retain the allowed count, Skip existing/Import all/Cancel choices and applicable owner/account/bot/chat/thread/binding/expiry validation. A prompt alone MUST NOT pause live delivery; only one selected run per conversation can become active.

#### Scenario: Prompt then cancel
- **WHEN** the owner requests history and cancels before selecting an import
- **THEN** no batch is fetched and live delivery continues normally.

#### Scenario: Competing or stale choices
- **WHEN** repeated, conflicting or out-of-scope controls attempt to select an import
- **THEN** they cannot start concurrent runs or replace an already selected mode/target.

### Requirement: Frozen complete and ordered plan
The import SHALL fetch and freeze the required batch, exclude wrong-conversation/revoked/Status rows, deduplicate source IDs, and order parents before children with deterministic tie/cycle handling. Its timezone, presentation style and checkpoints MUST remain stable through retry.

#### Scenario: Parent ordering and absent quote
- **WHEN** source timestamps tie or disagree with quote ancestry, or a parent lies outside the batch
- **THEN** the frozen plan preserves safe parent ordering and the existing quote-preview behavior without selecting a stale unrelated copy.

### Requirement: Destination-scoped recovery and fresh copies
Skip-existing SHALL honor destination completion; Import-all SHALL retain fresh run-scoped copies and replies. Accepted parts and day markers MUST be reused through partial recovery, including retained Telegram media and legacy checkpoint shapes.

#### Scenario: Cleared archive imported again
- **WHEN** Import all restores messages after destination copies were cleared
- **THEN** fresh mappings and replies refer to the new run's copies rather than old cleared receipts.

#### Scenario: Partial item or day-marker retry
- **WHEN** a run resumes after a definite refusal or restart
- **THEN** completed parts and retained date markers are not duplicated, and legacy runs keep their original presentation/checkpoints.

### Requirement: Scoped live hold and truthful completion
An active import SHALL hold only its conversation's live events; failure MUST release that hold under the existing run-state contract. Completion reporting MUST preserve the meaning of available messages checked rather than mislabeling it as newly sent messages.

#### Scenario: Failed batch with unrelated live work
- **WHEN** fetching or processing history fails
- **THEN** the run retains its recovery evidence, its live hold is released as specified and other conversations remain eligible.
