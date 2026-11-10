# Bifrost behavior specifications

This is the behavior baseline for the Go port on `port/golang`, sourced from commit `checkpoint-go-contract-baseline` (including the local .gitignore commit).

Start with the [capability and evidence index](traceability.md). Eleven main specs describe the current TypeScript implementation; the owner has approved preserving all current behavior, including linked-name formatting and intentional deletion rules, with conditional outgoing caption clearing as the sole message behavior change. Existing commands and presentation examples remain documented in [daily use](../README.md#daily-use) and [Telegram formatting](../docs/TELEGRAM-FORMATTING.md).

The open [define-go-behavior proposal](changes/define-go-behavior/proposal.md) records the target constraints already selected: the OpenWA Go SDK, go-telegram/bot, latest stable releases and applicable native features, PostgreSQL instead of SQLite, and Docker Compose. See the [target integration requirements](changes/define-go-behavior/specs/go-runtime-integration/spec.md).

The [owner review record](changes/define-go-behavior/review.md) records approval of all existing behavior and the caption-clearing exception. The behavior contract is ready for a separate Go implementation design. Keep the change open until its target requirements are implemented; documentation approval does not mean the Go runtime exists.

The [storage and interface reference](runtime-reference.md) records the SQLite state model, current defaults, HTTP response behavior and existing Compose boundary for migration review.

## Validation

- Bun 1.4.2 regression baseline: **263 passed, 11 skipped, 0 failed**. HTTP/media fixtures require loopback access.
- Docker transfer and codec fixtures remain opt-in and were not exercised.
- Run `openspec validate --all --strict --no-interactive` and `bun run check:repository` for documentation validation.
- Provider feature/version evidence and compatibility checks are in [the library review](library-review.md).

No private environment files, databases or deployment identity data are included.
