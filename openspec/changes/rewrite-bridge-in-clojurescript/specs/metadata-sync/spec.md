# Spec Delta

## Purpose

Refresh existing contact/group presentation and roster knowledge without mutating platform membership or overwriting shared destination identity.

## ADDED Requirements

### Requirement: Coalesced scoped synchronization
Manual, binding, linking, new-conversation and cron requests SHALL retain their scope, coalescing, waiters, target results and limited retries. Ordinary relay priority MUST remain ahead of metadata work.

#### Scenario: Concurrent requests for one scope
- **WHEN** several valid refresh requests target an already active scope
- **THEN** they share the existing work and retain the required completion notifications instead of creating duplicate provider mutations.

### Requirement: Validated roster and destination permissions
Roster replacement SHALL require a complete validated provider response. Inbox synchronization MUST preserve shared management-group identity, and mirror updates MUST verify destination/privacy/admin permissions without automatically adding or removing members.

#### Scenario: Incomplete roster or missing permission
- **WHEN** group lookup is incomplete or required Telegram permission is absent
- **THEN** valid cached identity/roster data is retained and the existing warning or grant/add guidance is produced without clearing membership knowledge.

#### Scenario: Inbox topic metadata refresh
- **WHEN** one WhatsApp group is represented inside the shared management forum
- **THEN** its topic can be updated without replacing the management group's title or identity.

### Requirement: Compatible photos and quiet cron
Photos SHALL retain allowlisted retrieval, content-hash reuse and existing absence behavior. Timezone-aware cron MUST persist one scheduled run per minute across restart and remain quiet when no meaningful changes or warnings occur.

#### Scenario: Rotated URL for unchanged photo
- **WHEN** a permitted image URL changes but its content does not
- **THEN** the existing content-hash cache prevents a redundant upload.

#### Scenario: Restart during a scheduled minute
- **WHEN** scheduling resumes in a minute already recorded as processed
- **THEN** the same cron run is not queued again and an unchanged successful run produces no unnecessary notice.
