# Spec Delta

## Purpose

Define the executable and operational contract for running, stopping and maintaining the bridge without changing its external services or identity.

## ADDED Requirements

### Requirement: Pinned offline-resolved startup
The application SHALL run with `bunx --bun nbb src/main.cljs` using pinned, preinstalled dependencies. Application and worker processes MUST verify Bun execution and preserve explicit local environment loading and Compose environment injection.

#### Scenario: Equivalent configured startup
- **WHEN** the operator supplies equivalent configuration through the local wrapper or Compose
- **THEN** the candidate starts under the pinned Bun runtime without resolving packages at service startup or falling back to Node or a JVM runtime.

#### Scenario: Wrong runtime
- **WHEN** an application or application worker is launched outside the required runtime
- **THEN** it refuses to process bridge work rather than silently selecting another runtime.

### Requirement: Single paced worker and durable scheduling
The bridge SHALL retain one paced worker and existing stream ordering, due times and retry classification. Durable work MUST remain in SQLite; concurrent ticks MUST NOT execute overlapping delivery work.

#### Scenario: Overlapping timer ticks
- **WHEN** another tick arrives while one job is executing
- **THEN** it does not claim or deliver a second job concurrently.

### Requirement: Controlled process and resource lifecycle
Shutdown SHALL stop acceptance and timers, drain active work within the shutdown allowance, and preserve conservative recovery if forced termination interrupts delivery. Codec subprocess isolation, descendant termination, temporary cleanup and existing system dependencies MUST remain supported.

#### Scenario: Graceful shutdown
- **WHEN** the operator sends a supported shutdown signal during normal operation
- **THEN** the process stops scheduling new work, finishes permitted active work and closes durable state safely.

#### Scenario: Interrupted long operation
- **WHEN** forced termination interrupts a send or codec process
- **THEN** restart preserves delivery uncertainty and the codec timeout/cleanup contract prevents orphaned descendants and intermediates.

### Requirement: Operational continuity
Setup, configure, identity inspection, review, synchronization, export/import and repository auditing SHALL retain their intended behavior. Export/import MUST preserve account identity, keys, receipts, pending updates and required volumes without starting restored bots.

#### Scenario: Restored installation
- **WHEN** the operator imports a valid stopped-installation export
- **THEN** the restored state remains compatible with the same identity and credentials and is not activated as a side effect of import.
