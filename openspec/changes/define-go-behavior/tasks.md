# Tasks

## 1. Baseline documentation

- [x] Create port/golang from current HEAD and initialize repository-local OpenSpec.
- [x] Record eleven capability baselines, source/fixture evidence and verification limits.
- [x] Record required provider clients, latest release policy, PostgreSQL and Docker Compose target constraints.
- [x] Run the Bun regression baseline: 263 passed, 11 skipped, 0 failed.

## 2. Desired behavior review with the owner

- [x] Review routing, member bots, topics, group linking, unmirroring and authorization; preserve route types and intentional old-topic deletion.
- [x] Review sends, replies, edits, formatting, mentions, reactions and deletion.
- [x] Review attachments, stickers, manual history and Status feeds.
- [x] Review ordering, retries, deduplication, uncertainty, restart recovery and read receipts.
- [x] Review ingress, configuration, sync, scheduling, cleanup and identity transfer.
- [x] Add agreed behavior deltas and acceptance scenarios; resolve all open review questions.

## 3. Validation and handoff

- [x] Validate all specs and change artifacts with OpenSpec strict validation and audit tracked and new documentation files for private data.
- [x] Record preliminary SDK feature coverage and integration verification needs, including explicit empty-caption serialization.
- [ ] During the separate Go implementation design, verify every required operation against exact latest released SDK/gateway/API versions, including conditional caption clearing.
- [x] Confirm the desired behavior contract is complete before beginning a separate Go implementation design.
