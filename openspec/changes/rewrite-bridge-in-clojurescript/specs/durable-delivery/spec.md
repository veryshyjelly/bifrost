# Spec Delta

## Purpose

Preserve account identity and delivery knowledge across retries, crashes, runtime changes and recovery by either implementation.

## ADDED Requirements

### Requirement: Stable account and conflict-safe identities
The bridge SHALL preserve `BIFROST_ACCOUNT_ID` independently of the transport session, reject explicit restored-account mismatches and reject conflicting aliases. Contacts, conversation identities and group authors MUST remain distinct.

#### Scenario: Transport session changes
- **WHEN** valid restored state is reopened with a new transport session
- **THEN** the saved account, account-scoped hashes, receipts and workflow identities remain stable.

#### Scenario: Conflicting identity
- **WHEN** configuration contradicts the saved account or an alias is reassigned to another canonical contact
- **THEN** the bridge refuses the conflicting change and retains existing identity evidence.

### Requirement: Compatible durable representation
The candidate SHALL preserve existing keys, mappings, receipt results, omitted/null JSON fields, hash input bytes and encrypted credential layout. Schema extensions MUST be backward compatible; old code MUST safely continue candidate-written jobs, states and checkpoints.

#### Scenario: Bidirectional continuation
- **WHEN** either implementation writes a synthetic database and the other reopens it
- **THEN** both can continue supported work against the latest state without dropping receipts or reinterpreting identity.

#### Scenario: Credential integrity
- **WHEN** an old or candidate credential is decrypted by the other implementation
- **THEN** valid synthetic plaintext is recovered and altered authentication data is rejected without exposing credentials.

### Requirement: Ordered queue and bounded retries
Job deduplication, priority, due times, attempt accounting and stream ordering SHALL remain compatible. Failed or uncertain predecessors MUST block their stream; unrelated streams and independently queued read/cleanup work MUST remain eligible under existing scheduling rules.

#### Scenario: Refusal and exhausted retries
- **WHEN** a documented retryable refusal occurs repeatedly
- **THEN** the job follows provider/backoff delays and the existing eight-attempt limit, eventually failing without bypassing its blocked successors.

### Requirement: Durable external-effect knowledge
An effect SHALL be recorded as sending before external mutation and done with its result after success. A done effect MUST reuse its receipt; ambiguous outcomes MUST remain uncertain, while definite refusal retains the existing retry classification.

#### Scenario: Replay after receipt persistence
- **WHEN** a completed effect is encountered again after a crash or retry
- **THEN** its saved result is reused without repeating the external mutation.

#### Scenario: Crash around remote acceptance
- **WHEN** termination leaves an effect sending and its job running
- **THEN** recovery changes the effect to uncertain and the job to pending, and automatic processing cannot resend the uncertain effect.

### Requirement: Explicit uncertainty review
Uncertain sends SHALL require explicit review before replay. Confirmation of no delivery MUST be required for uncertain-job retry; review MUST preserve done receipts and affect only the intended job and its stable outgoing effect key.

#### Scenario: Unconfirmed retry
- **WHEN** an authorized reviewer requests uncertain retry without explicit confirmation of no delivery
- **THEN** the request is refused and receipts and uncertainty remain unchanged.

#### Scenario: Scoped confirmed review
- **WHEN** the reviewer confirms no delivery for one job
- **THEN** only its uncertain effects become retryable, preserving similarly prefixed jobs, other private bots and done effects.

### Requirement: Latest-state rollback
Rollback SHALL preserve candidate-written receipts and workflow progress and resume old code against the latest compatible database and the same identity/volumes. An unsafe compatibility conflict MUST block release rather than permit stale restoration or automatic uncertain replay.

#### Scenario: Candidate shutdown followed by rollback
- **WHEN** an authorized rollback follows candidate processing
- **THEN** the candidate stops first and old code continues current compatible state without forgetting new deliveries.
