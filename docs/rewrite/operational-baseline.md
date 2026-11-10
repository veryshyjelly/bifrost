# Operational, ignored-update and legacy baseline

OpenSpec task 1.10 records **17 cases and 99 observed transitions** against immutable baseline `checkpoint-clojure-baseline`: 12 unchanged original assertions and five supplemental ignored/legacy cases. Run `bun scripts/rewrite/operational-baseline.ts check`. Independent full transcripts must match each other and the committed [manifest](../../test/rewrite/fixtures/operational-baseline.json). Ordered helper/provider arguments/results/errors, generated synthetic file bytes/modes and durable tables regenerate at ignored `data/rewrite/evidence/operational-baseline.json`.

| Coverage | Original inventory sources |
|---|---|
| Fresh independent synthetic secrets, private file modes, preservation of existing environment bytes, literal dollar signs/comments and legacy configuration | Both original `test/configure.test.ts` cases |
| Ready/disconnected setup checks, hosted logout before local Telegram calls, permission validation, webhook installation and Status startup order | Both original `test/setup.test.ts` cases |
| Literal credentials, identity/default local path restoration, retired n8n setting removal, volume discovery, ambiguous/bind-mounted rejection | Two default original `test/transfer.test.ts` cases |
| Permanent versus transport identity, real SQLite reopening, credential rotation, conflicting identity refusal, history holds and uncertain-send preservation | All five original `test/identity.test.ts` cases |
| Legacy n8n/direct-management convergence without forwarding HTTP or duplicate read receipt | Original `test/bridge.test.ts:45` |
| Unhandled Telegram envelopes, bot/foreign actors, admitted ignored WhatsApp scopes and malformed session/event/bot/update refusal | Three supplemental [operational-scenarios.ts](../../scripts/rewrite/operational-scenarios.ts) cases |
| Unknown job completion and malformed legacy forwarding with absent versus null IDs | Two supplemental cases in the same file |

Original setup/configure/transfer exports execute unchanged. Process-local wrappers observe their calls and results. Setup receives the original test's scripted bridge, with its provider/lifecycle methods observed; no real webhook or account is contacted. Configuration observation requires the original `bridge-configure-*` directory beneath the real OS temp root and records `bridge.env`, `telegram-api.env` and retained `n8n.env` bytes/modes. Only that random temporary parent is represented as `$CONFIG`; fixed test entropy and synthetic credentials never leave the recorder as application configuration.

The original worker completes an unknown job type without an API call. A malformed `n8n` job is also completed after enqueuing its unchanged payload: absent ID yields `management:undefined`, explicit null yields `management:null`; the subsequent unsupported management action fails and retains its delivery-problem work. These observations are parity evidence, not approved corrections. Legacy valid forwarding convergence and uncertain effects remain independently covered.

The two opt-in Docker transfer tests are explicitly excluded from this transcript family and remain pending Gate 7 archive/volume validation. They are not represented as passing transfer rehearsals. The full default suite still reports their skips, alongside nine codec opt-ins. No production volume, private export or real configuration is used. Candidate operational comparison and all eight acceptance gates remain pending.
