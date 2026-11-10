# Correctness investigation register

Authority: [implementation specification, Correctness policy](08-implementation-specification.md). Baseline for every entry: `checkpoint-clojure-baseline`. These are **candidates**, not approved exceptions or implemented corrections. Offline characterization is authorized; no observable application behavior has changed. Original and candidate comparison must continue to expect parity until a specific correction has the required evidence and authority.

The source behavior is described in [document 02](02-behavioral-specification.md) and [historical questions](07-open-questions.md). Document 08 governs later evaluation. Reproduce all three with `bun scripts/rewrite/action-baseline.ts check`; exact requests, per-transition state and retained controls are fingerprinted in [action-baseline.json](../../test/rewrite/fixtures/action-baseline.json).

| Required field | KEEP-001 | EDIT-001 | CALLBACK-001 |
|---|---|---|---|
| Status | Candidate, baseline characterized | Candidate, baseline characterized | Candidate, baseline characterized |
| Baseline reference | `src/bridge.ts:874`, `manage` keep/mute/ignore branch | `src/bridge.ts:483`, `waChange` per-copy edit loop; first formatted part selected at line 499 | `src/bridge.ts:530`, generic action lookup; owner admission at line 83 |
| Reproducer | [action-scenarios.ts:36](../../scripts/rewrite/action-scenarios.ts:36) | [action-scenarios.ts:44](../../scripts/rewrite/action-scenarios.ts:44) | [action-scenarios.ts:56](../../scripts/rewrite/action-scenarios.ts:56) |
| Observed behavior | Mirror mode `group` becomes `muted`; keep sets mode `inbox` while retaining the mirror destination | A two-part original receives the same first edited text part in both distinct Telegram copies | Owner callback in another destination with an old-session/expired action still executes mute/keep; the action remains reusable |
| Defect/invariant investigation | Not yet demonstrated: determine whether mode denotes delivery scope or only presentation after resuming, and whether existing route behavior violates documented conversation intent | First-part repetition is reproduced. Determine the intended edit contract across current, extra and missing copies, preserving attribution and reply/media scope | Weaker validation is reproduced. Document 02 explicitly forbids silently importing specialized expiry/context rules; establish the applicable security/domain invariant before choosing a delta |
| Correction / exact expected delta | None selected | None selected | None selected |
| Risks to assess | Mirrored routing, history targets, mute/resume and persisted legacy modes | Truncation/duplication, copy mapping, media labels, removed parts, stale quotes, partial/uncertain edits and retry keys | Stale controls, scope/ownership, callback replay, bot-private message IDs and compatibility with retained keyboards |
| Verification | Original characterization and exact transcript/state checks only. Candidate regression, unaffected-trace comparison and latest-state rollback checks pending | Same; original first-part repetition is asserted explicitly, rather than changing the golden to imply a correction | Same; retained action row and repeated owner callback execution are asserted explicitly |
| Approval | No correction approved. Any later autonomous correction must demonstrate the Q11 invariant and record its exact delta/rationale; ambiguous semantics require owner decision | Same | Same |

No entry is demonstrated, corrected or verified merely because its current behavior looks suspicious. No expected delta has been allowlisted. Registration's partial-setup prompt omission is separately characterized in [control baseline](control-baseline.md); it has not been proposed as a correction.
