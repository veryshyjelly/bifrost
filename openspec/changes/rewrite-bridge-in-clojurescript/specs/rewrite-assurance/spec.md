# Spec Delta

## Purpose

Make migration acceptance traceable to independent evidence and explicit operational authorization rather than inferred from code progress or planning status.

## ADDED Requirements

### Requirement: Exhaustive baseline and independent replay
The migration SHALL freeze the investigated source and hashes, preserve its original tests and independently runnable oracle, and cover the complete document 02 inventory. Original and candidate MUST replay identical synthetic inputs independently with provider network access disabled.

#### Scenario: Partial candidate coverage
- **WHEN** component tests pass but some inventory workflows or recovery paths lack candidate replay
- **THEN** those items remain open and cannot be described as full parity or a completed release gate.

#### Scenario: Representation normalization
- **WHEN** replay comparison normalizes nondeterministic representation
- **THEN** it preserves identity relationships, raw signed/hashed bytes, missing/null distinctions, effect order, scope and delivery safety semantics.

### Requirement: Evidence-backed correctness exceptions
Observable divergence SHALL require the governing exception register with source/reproducer, violated contract, exact delta, risk, regression/replay/rollback evidence and authority. Critical or migration-relevant demonstrated violations follow Q11; ambiguous semantics MUST receive a focused owner decision, and unrelated noncritical issues MUST be deferred.

#### Scenario: Known suspicious behavior
- **WHEN** keep routing, split-message edits or generic callbacks are investigated
- **THEN** source, tests and invariants establish the result before any correction is approved, and unaffected traces remain strictly compared.

### Requirement: Eight independent acceptance gates
All eight gates in document 08 SHALL retain their acceptance criteria and evidence ledger. Each check MUST record source/runtime/image/dependency identity, command/environment/time, outcome, skips, artifacts and open work. Mocks, historical checks and OpenSpec readiness MUST NOT substitute for required current executable or provider evidence.

#### Scenario: Runtime or codec capability remains unverified
- **WHEN** required Linux runtime paths, codecs or opted-in transfer/restore checks lack passing current evidence
- **THEN** the corresponding gate remains pending, failed or blocked rather than passing through default-suite success.

### Requirement: Recovery and sustained workload acceptance
Failure testing SHALL exercise durable transitions and current-state old/new continuation. The planned soak MUST include a real 24-hour elapsed synthetic observation and accelerated boundaries, recording workload, resource use, latency and backlog recovery without inventing a performance target.

#### Scenario: Unbounded growth or unsafe rollback
- **WHEN** resource growth is unexplained, backlog does not recover, or old code cannot safely resume latest candidate-written state
- **THEN** release remains blocked until the material risk is resolved and verified.

### Requirement: Authorized filtered capture and single-writer canary
Real recordings SHALL come only from separately authorized dedicated test conversations, filtered at collection and held privately before sanitization. Canary execution MUST follow a concrete reviewed plan and the complete document 08 checklist, with one live writer per identity even across separate databases.

#### Scenario: Unrelated conversations in a provider batch
- **WHEN** collection cannot exclude unrelated content safely at its source
- **THEN** it uses synthetic fixtures or an appropriately isolated test identity instead of capturing that unrelated content.

#### Scenario: Canary failure
- **WHEN** a harmless presentation/control check fails
- **THEN** any continued observation is monitored and recorded without waiving acceptance; credible integrity/security risk stops affected activity, and ambiguous sends remain blocked for review.

### Requirement: Separate release approval and safe rollback
Canary completion SHALL NOT authorize production cutover. Release MUST require the full gate/exception/compatibility/deployment-difference report and explicit approval. Cutover and rollback MUST enforce one writer, consistent latest state, preserved identity/keys/volumes and reconciliation before any corruption-driven restore.

#### Scenario: Cutover request
- **WHEN** the candidate is proposed for production
- **THEN** the operator can review the concrete maintenance sequence, current backup, pending work, matching old image and latest-state rollback rehearsal before authorizing execution.

#### Scenario: Integrity or compatibility blocker
- **WHEN** critical delivery/security defects, unexplained divergence or incompatible persistence remain
- **THEN** neither a promising canary nor predominantly passing tests permit release.
