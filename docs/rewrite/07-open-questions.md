# Genuine unresolved questions

Resolution update (2026-11-10): no owner-level architecture questions remain. Q1 selects the investigated repository baseline; Q2/Q3/Q5 select synthetic tests, recordings only from dedicated test conversations and a controlled canary. The twelve decisions and governing execution requirements are in [08 Implementation specification](08-implementation-specification.md). The alternatives/questions below are retained as investigation history, not pending choices. Concrete test identities, timing and operational authorization are execution prerequisites; no implementation, collection or deployment is authorized by this update. Correctness candidates are governed by Q6/Q8/Q10/Q11/Q12 rather than unconditional preservation.

Language, nbb exclusively on Bun, external OpenWA and existing Telegram/Managed Bots integration are settled. No questions about intended existing behavior are needed. Recommendations below resolve routine architecture choices without asking the owner to rediscover source code.

## 1. Which running revision defines cutover parity?

Current code coordinates management internally and converts legacy n8n jobs (`src/bridge.ts:99`, `123`). `docs/CURRENT-DEPLOYMENT.md:3` says the home deployment uses an external Compose checkout and legacy image forwarding management through n8n. That deployment claim is **D** here: the running host, workflow and image were not inspected. Repository behavior and live behavior may differ.

Options: (a) target investigated repository revision; (b) target an independently recorded deployed revision; (c) update old deployment to repository baseline, verify, then rewrite against it. Recommend (a), with deployed image/revision inspection and replay comparison before any live cutover. This keeps the plan reviewable and avoids accidentally reintroducing n8n. Option (b) may require adding undocumented legacy behavior; (c) adds a distinct deployment migration and risk. **Owner decision needed if live revision differs:** confirm repository baseline or explicitly include deployed-only differences after evidence collection. Do not silently claim both have parity.

## 2. Authorized real-event corpus and live verification window

Current tests construct fake APIs and synthetic events; webhook ingress stores payloads durably in jobs (`src/store.ts:54`, `src/server.ts:32`). No sanitized real replay corpus was found among tracked files. External behaviors—official Managed Bot event shape/permissions, Telegram picker UI, OpenWA fork reconnection and retained media—cannot be completely proved by mocked tests. User reports Managed Bots working; this investigation neither disputes nor independently revalidates it.

Options: (a) synthetic-only offline acceptance; (b) private sanitized recordings plus later controlled single-writer canary; (c) immediate live exploration. Recommend (b). It offers stronger evidence while keeping credentials/content private and postponing live side effects until implementation is approved. Option (a) leaves provider assumptions unresolved; (c) changes live accounts before a candidate is ready. **Owner input needed later:** authorize the test contact/group and maintenance window, and identify any content that must never be captured. No live access is required to review this architecture.

## Engineering verification outstanding, not owner design decisions

Full nbb adapter/stream/codec/lifecycle/soak compatibility is U; use document 04 gates. nbb candidate 1.6.214 ran on Bun locally, but needs pinned target-image validation. core.async is unnecessary and unavailable by default in the tested installation: recommend Promises/bundled Promesa. Preserve SQLite schema/keys/AES layout and one worker; no owner choice is required for those recommendations.

Legacy generic callback actions lack the expiry/context validation of history/Status/mention workflows (`bridge.ts:466`, `516`). Route keep semantics and split-message edit behavior also have literal quirks (document 02/03). Recommend characterize and preserve for this rewrite; propose any corrections separately after parity. Do not turn these into hidden feature changes or force a product decision now.
