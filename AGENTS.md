# Repository Guidelines

## Project Structure & Module Organization

Bifrost is a personal WhatsApp–Telegram bridge. `src/` contains the Bun/TypeScript runtime: `bridge.ts` routes events, `store.ts` persists queues and effects in SQLite, and focused modules handle history, media, mentions and controls. `scripts/` holds setup, transfer and Python codec helpers. Tests live in `test/*.test.ts`; operational documentation lives in `docs/`. Runtime assets belong in ignored `data/`, `media/` and `backups/`.

Migration progress is tracked in [OpenSpec tasks](openspec/changes/rewrite-bridge-in-clojurescript/tasks.md), governed by [the implementation contract](docs/rewrite/08-implementation-specification.md).

## Build, Test, and Development Commands

Use Bun 1.4.2, matching `.bun-version`.

- `bun install --frozen-lockfile`: install pinned dependencies.
- `bun run configure`: generate or synchronize private configuration; follow `docs/INSTALL.md` for fresh setup.
- `bun run start`: run the bridge with `bridge.env`; Telegram API and OpenWA must be reachable.
- `docker compose build bifrost telegram-api`: build both service images.
- `docker compose up -d bifrost telegram-api`: start configured services.
- `bun run test`: run the default regression suite.
- `bun run test:stickers`: run codec fixtures; dependencies are documented in `docs/STICKERS.md`.
- `bun run test:transfer`: run Docker export/import tests using the built Bifrost image.
- `bun run check:repository`: audit tracked files for credentials and private artifacts.

## Coding Style & Naming Conventions

Use two-space indentation, single quotes and semicolons in TypeScript; use four spaces in Python. Keep ESM imports with explicit `.ts` extensions. Use camelCase for functions/variables, PascalCase for classes/types, and descriptive filenames such as `sticker-worker.ts`. No formatter or linter is configured; match surrounding code.

## Testing Guidelines

Tests use `node:test` and `node:assert/strict` under Bun. Name files `*.test.ts` and describe observable behavior in test names. Run focused tests with `bun test ./test/bridge.test.ts`. Cover changed routing, retries, partial/uncertain delivery and owner checks using fixtures. HTTP tests bind loopback; codec and Docker tests are opt-in. No numeric coverage threshold is configured.

## Commit & Pull Request Guidelines

Match Git history’s plain, imperative subjects, for example `Preserve Bifrost identity across OpenWA session changes`. Keep commits focused. PRs should describe changed behavior, link related issues when applicable, list validation performed, and update relevant README/docs. Include screenshots for Telegram presentation changes when useful.

## Security & Configuration

Keep tokens, private environment files, SQLite databases, media and backups out of Git. Preserve `BIFROST_ACCOUNT_ID`, encryption keys, delivery receipts and Docker volumes during changes. Follow `docs/MOVING.md` before importing an identity. Preserve stable idempotency keys and explicit review of uncertain sends.

## Agent Workflow and Token Efficiency

Minimize total tokens needed for a verified result. Preserve correctness, required checks and security boundaries. Research and examples: [Agent efficiency](docs/AGENT-EFFICIENCY.md); read it only when needed.

- Use `apply_patch`, `rg`, shell operations and Bun directly. No Python heredocs or language wrappers for routine reads, searches, rewrites, JSON summaries or MCP calls. Python project/codec work remains supported.
- Call native MCP tools directly; never build a substitute stdio client or repeatedly initialize servers. If tools are absent, diagnose once and report the required client refresh. Registration alone does not prove availability.
- Reuse native Clojure MCP/REPL tools when available. Verify Bun execution explicitly; REPL checks do not replace pinned Bun/Linux tests.
- Find paths with scoped `rg --files`, locate symbols with `rg -n`, then read bounded ranges. Avoid generated oracle/build trees and unnecessary whole-file reads.
- Reuse unchanged context; inspect diffs or missing sections instead of rereading specifications, skills and sources.
- Filter output before returning it: relevant fields, counts, hashes, paths and failures. Save complete logs on disk. Start ordinary calls around 1,000–2,000 output tokens; narrow queries when truncated.
- Never dump catalogs, schemas, fixtures, lockfiles, base64, manifests or page navigation. Discover matching names first; inspect only the selected schema/fields using `jq` or a short Bun expression.
- Batch independent reads/checks and inspect every result; keep dependencies and edits sequential. Reuse processes and avoid frequent empty polls. Parallelism saves latency, not necessarily tokens.
- Patch focused units; check namespace loading before dependent edits. Reuse helpers; write scripts only for substantial reusable operations.
- Test focused paths first, then required broader checks once per relevant snapshot. Repeat for changes/failures only. Use `bun test ./test/...` to avoid matching generated copies; preserve exit codes and skip counts.
- Reuse Docker/dependency caches and verified results. Rebuild for changed image inputs; retain source/image hashes.
- Research narrow, domain-filtered primary sources; fetch relevant sections and retain links instead of repeating searches.
- Keep updates brief and limited to new findings/blockers. Link artifacts; avoid repeating code, plans and results.
- Keep AGENTS.md stable and compact; link detailed notes. Checkpoint decisions, evidence and next steps. Do not alter model/reasoning/compaction/global settings just to claim savings; use actual usage telemetry when available and never invent percentages.
