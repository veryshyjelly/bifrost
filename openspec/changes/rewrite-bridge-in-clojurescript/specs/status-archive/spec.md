# Spec Delta

## Purpose

Archive WhatsApp Status posts in the manager feed without turning archive interactions into outgoing messages or conversation routing preferences.

## ADDED Requirements

### Requirement: Dedicated validated read-only feed
Status SHALL use the existing account-scoped manager topic, authenticated media endpoint, author attribution, timestamps and supported content classes. Malformed/expired source posts MUST be excluded and refresh/webhook races deduplicated without creating ordinary conversations.

#### Scenario: Refresh overlaps a webhook
- **WHEN** the same valid Status arrives through startup/refresh and a webhook
- **THEN** exactly one archive delivery is recorded under the existing source identity.

#### Scenario: Interaction inside the archive
- **WHEN** the owner types, edits, reacts or uses an unrelated message action in the Status topic
- **THEN** no WhatsApp Status publication or ordinary outgoing chat send is created.

### Requirement: Independent revision-safe author preferences
Status mutes SHALL apply to canonical authors before media retrieval and remain independent of conversation mute/routing. Controls MUST preserve account/owner/bot/topic/expiry/revision checks and survive aliases and reopening.

#### Scenario: Stale unmute or retry
- **WHEN** an old control or retried confirmation conflicts with a newer preference
- **THEN** the newer author's mute decision cannot be undone by that stale action.

#### Scenario: Author becomes unmuted
- **WHEN** the owner validly unmutes an author
- **THEN** future eligible posts resume without replaying posts previously skipped while muted.

### Requirement: Retained archive and scoped clearing
Archived Status copies SHALL remain after source expiry. Expired or unavailable media and topic clear operations MUST retain their existing presentation and scope without deleting unrelated conversations or changing author preferences implicitly.

#### Scenario: Expired original and later cleanup
- **WHEN** an archived source expires or the owner clears recorded Status content
- **THEN** expiry does not itself remove the retained archive, and explicit clearing affects only its permitted topic records.
