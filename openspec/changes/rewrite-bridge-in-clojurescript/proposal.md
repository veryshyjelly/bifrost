# Proposal

## Why

The bridge rewrite already has finalized decisions and verified implementation progress, but OpenSpec has no project requirements or active migration tasks. Adopt the governing contract into one traceable change so subsequent work preserves the existing behavior, completed evidence and explicit release boundaries.

## What Changes

- Deliver the authorized rewrite with functional ClojureScript, nbb exclusively on pinned Bun, immutable decisions and explicit I/O dependencies.
- Preserve the investigated TypeScript revision as an independently runnable oracle, retaining its tests, durable identity, receipts, checkpoints and provider contracts.
- Track all behavioral inventory items, eight acceptance gates, correctness exceptions and current-state rollback evidence in OpenSpec tasks and the existing evidence ledger.
- Carry forward completed storage, pure rules, adapters, ingress, text relay, mention transforms, worker and modeled crash recovery; finish remaining workflows progressively.
- Change the operational application entry to `bunx --bun nbb src/main.cljs` only after the actual implementation and required compatibility evidence exist. Preserve explicit local environment loading and Compose injection.
- Keep collection, canary and production cutover as separately authorized operations. No live actions or application changes are part of this planning setup.

## Capabilities

### New Capabilities

These are new OpenSpec coverage, not new product features. Existing bridge behaviors are codified alongside the required runtime and migration-assurance contract.

- `bridge-runtime`: Bun/nbb execution, dependency installation, process lifecycle and operational tooling.
- `durable-delivery`: account identity, storage/encryption compatibility, queue ordering, effects, receipts and conservative recovery.
- `authenticated-ingress`: webhook authentication, admission, authorization and explicit delivery review.
- `message-relay`: scoped routing, replies, formatting, mentions, transports, media and structured contents.
- `managed-bots`: official creation/token events, explicit contact binding, readiness and inbox retirement.
- `conversation-controls`: group picker/link/unmirror, commands, controls, menus and scoped cleanup.
- `message-actions`: edits, reactions, revocations and poll interactions.
- `history-import`: choices, frozen import plans, scoped copies and resumable history delivery.
- `status-archive`: read-only Status feed, expiry, refresh deduplication and author mute controls.
- `metadata-sync`: explicit roster/metadata refresh, coalescing, permissions and quiet scheduling.
- `rewrite-assurance`: oracle replay, exception policy, evidence gates, soak, canary, cutover and latest-state rollback.

### Modified Capabilities

None. `openspec list --specs` is empty; no existing OpenSpec requirements are replaced.

## Impact

Candidate namespaces under `src/bifrost/`, the eventual `src/main.cljs`, dependencies, deployment packaging, operational scripts and replay tests are affected. Existing OpenWA/Telegram services and codec/browser dependencies remain. The normative authority is [implementation specification](../../../docs/rewrite/08-implementation-specification.md); [behavioral inventory](../../../docs/rewrite/02-behavioral-specification.md) remains exhaustive. Implementation checkpoint `checkpoint-clojure-evidence` and the existing source-hashed check reports establish current progress. No schema replacement, new service, automatic membership changes, unsupported-event forwarding or production deployment is proposed.
