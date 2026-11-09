# Repository Guidelines

## Project Structure & Module Organization

Bifrost is a personal WhatsApp–Telegram bridge. `src/` contains the Bun/TypeScript runtime: `bridge.ts` routes events, `store.ts` persists queues and effects in SQLite, and focused modules handle history, media, mentions and controls. `scripts/` holds setup, transfer and Python codec helpers. Tests live in `test/*.test.ts`; operational documentation lives in `docs/`. Runtime assets belong in ignored `data/`, `media/` and `backups/`.

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
