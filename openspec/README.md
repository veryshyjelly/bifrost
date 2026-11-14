# Bifrost behavior specifications

These specifications describe the current Go runtime. Preserve routing,
presentation, identity scope, deletion choices and delivery recovery while making
changes. The owner-approved caption behavior, sticker budget and restricted-bot
mention fallback are included in the current implementation.

Start with the [capability index](traceability.md),
[current runtime requirements](specs/go-runtime-integration/spec.md),
[storage and interface reference](runtime-reference.md) and
[runtime lifecycle](../docs/RUNTIME.md). Proposed changes under `changes/` record
specific behavior updates. Completed source history remains available through Git.

Run `make spec-check` for strict specification validation and `make check` for
formatting, vet, race tests, repository auditing, Compose and SQL generation.
Use isolated PostgreSQL for persistence/concurrency tests; do not run tests against
the live account. Provider readiness and a successful send acknowledgement do not
prove every workflow or recipient rendering. See [validation evidence](../docs/VALIDATION.md).

No private environments, credentials, databases, media or deployment identity data
belong in this specification tree.
