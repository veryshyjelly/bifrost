# Agent efficiency research and working examples

Researched 2026-11-10. The policies in [AGENTS.md](../AGENTS.md) apply to repository work; this note is an on-demand reference, not required reading on every turn.

## Findings from this session

The avoidable costs were repeated source/specification reads, broad searches and truncated output, handwritten MCP transport wrappers with repeated initialization/schema dumps, verbose passing-test logs, and large edits followed by delimiter repair. These are observed workflow problems, not measured billing totals.

| Avoidable pattern | Preferred operation |
|---|---|
| `python3 - <<'PY'` to read or replace source | `rg`/bounded `sed` for discovery; `apply_patch` for edits |
| A Python or Bun client implementing MCP initialization and JSON-RPC | Native MCP calls; one setup diagnosis if tools are missing |
| Printing every matching tool's description and schema | Discover names, select a tool, inspect only its required schema |
| `cat` on several large source files or the entire evidence ledger repeatedly | Locate the symbol and read its surrounding range; reuse prior context |
| Printing an entire JSON manifest to obtain one fingerprint | `jq -r '.candidate.sourceFingerprint' <report>` |
| Dumping complete passing-test output or giant assertion diffs | Preserve the full private log, return exit status/counts, then read the relevant failure range |
| `bun test test/rewrite/foo.test.ts` matching generated copies | `bun test ./test/rewrite/foo.test.ts` |
| Rebuilding an unchanged image or rerunning an unchanged passing suite | Reuse results tied to the same inputs; repeat when relevant inputs change |
| Repeated broad web searches and page-navigation output | A narrow primary-source query, then the relevant section |
| Recreating a plan or implementation transcript in each update | State the new finding and link the durable artifact |

Python syntax is not inherently more token-expensive than another language. The repository policy removes unnecessary wrappers, quoting, duplicated transport logic and output, while keeping routine operations on the existing toolchain. Legitimate Python codec implementation/testing remains supported.

## Practical sequence

1. State the next concrete uncertainty. Search for the relevant file/symbol rather than rereading the repository.
2. Read enough surrounding code to understand its contract and callers. Do not save tokens by guessing omitted behavior.
3. Apply a focused patch. For ClojureScript, check loading or a small REPL expression before building on the change.
4. Run the relevant behavioral test and inspect its exit status. On failure, read the diagnostic range from its saved log. After focused checks pass, run the required wider validation.
5. Record source revision/fingerprint, command, outcome, skips and artifact path once. Reuse this evidence while its inputs remain unchanged.
6. Report the outcome briefly, including incomplete work and material uncertainty.

For recorded rewrite suites, reuse `bun scripts/rewrite/check.ts local` or `linux`: these already save full logs and emit short summaries. These commands still require the appropriate environment and current image; they do not waive acceptance gates. For ad hoc checks, redirect output to a private log and preserve the command's exit code. Do not pipe through `tail` in a way that replaces a failing test's status with a successful filter's status.

## Research basis and limits

- The [OpenAI Codex Prompting Guide](https://developers.openai.com/cookbook/examples/gpt-5/codex_prompting_guide) favors standard task tools, scoped searches and tool interfaces close to the underlying operations. The concrete repository rules above are our application of that guidance, not a universal ban on scripting.
- [Official MCP documentation](https://learn.chatgpt.com/docs/extend/mcp?surface=app) distinguishes configuring servers from connecting clients and documents tool allow/deny lists, startup timeouts and per-tool output limits. Keep only necessary servers/tools enabled when configuring an integration; do not change global settings as a side effect of repository work. Native availability must be checked in the current session.
- [OpenAI prompt-caching guidance](https://developers.openai.com/api/docs/guides/prompt-caching) explains reuse of matching prompt prefixes and the effects of changing instructions/tool definitions/history. Stable concise instructions help avoid unnecessary churn. Caching can reduce repeated-input processing cost/latency; it does not remove text from the context window or shorten newly generated output.

These are distinct optimization targets: fewer input/output tokens, lower billed cost, fewer round trips and shorter elapsed time. Batching calls primarily affects round trips; caching primarily affects repeated-input processing. Neither guarantees fewer generated tokens. Reducing noisy output before it reaches the model directly reduces the context carried forward.

Exact savings require usage telemetry, including input/output/cached/reasoning tokens where exposed. Command-output estimates and file sizes are only proxies. Compare equivalent completed tasks and unchanged acceptance criteria. Do not add a new measurement framework, repeatedly compact/restart sessions, disable useful tools, reduce reasoning settings or delegate full-context tasks solely on the assumption that it will be cheaper. Prefer the simplest verified workflow; use specialized tools when they eliminate real work or prevent repeated errors.
