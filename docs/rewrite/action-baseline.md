# Edit, reaction, revocation and poll baseline

OpenSpec task 1.7 records **54 cases and 855 observed transitions** against immutable baseline `checkpoint-clojure-baseline`: 48 unchanged original fixture assertions and six supplemental scenarios. Run `bun scripts/rewrite/action-baseline.ts check` to reproduce exact full transcripts twice and verify the committed [manifest](../../test/rewrite/fixtures/action-baseline.json). Full API requests/results/errors, logs and durable tables regenerate in ignored `data/rewrite/evidence/action-baseline.json`.

The recorder reuses the [control baseline observation harness](control-baseline.md); original source, original assertions and fixture provider methods remain unchanged. A shared manifest helper avoids duplicate replay/verification code. The preexisting control transcript digest remains unchanged after this refactor. Only fixed clock/entropy and process-local observation are injected. Provider fetch fails closed; Linux verification disables provider networking independently.

| Coverage | Inventory references |
|---|---|
| Native reactions, member/master selection, removals, snapshots, private scope, split/cleared copies, duplicate/echo handling, known refusal/ambiguity, custom emoji and repair | Every original case in `test/reactions.test.ts` |
| Own-message Keep/Delete, copy scope, retained choices, stale/foreign/expired controls, tombstones, default keep, delete refusal, repeated controls and ambiguous keyboard updates | Every original case in `test/revocations.test.ts` |
| Cleared-copy edits, participant attribution, private edit fallback, revoked-before-original/history suppression and child-bot callbacks | Five named original `test/bridge.test.ts` cases selected in [action-baseline.ts](../../scripts/rewrite/action-baseline.ts) |
| Verified self identity and historical edit/caption entities | Two named original `test/format.test.ts` cases selected in the same file |
| Poll single/multiple toggling and admitted `poll_answer` no-op; late edits, outbound echoes and revocation suppression | Three supplemental [action-scenarios.ts](../../scripts/rewrite/action-scenarios.ts) cases |
| Keep-routing, split-message edit and generic callback behavior | Three supplemental characterization cases, linked in the [candidate register](exception-register.md) |

Original inventory IDs and source hashes are retained in every original-case manifest entry. The independent-process regression checks all 54 cases, exact transcripts/state, candidate characterization presence, duplicated first edit part and completion of `poll_answer` without a WhatsApp send. The supplemental scenarios use original effect, mapping, queue and admission logic.

The three correctness candidates are characterized without selecting or preapproving corrections. No golden difference or new application behavior is allowed. Candidate implementation/comparison and latest-state rollback evidence remain pending, as do all eight release gates.
