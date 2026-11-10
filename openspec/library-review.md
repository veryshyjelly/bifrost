# Required libraries and feature review

Checked 2026-11-10. Version observations are provisional: resolve and pin latest stable versions when implementation starts, then recheck before completion. No Go runtime dependencies have been installed in this phase. A later direct Go module proxy check resolved OpenWA SDK v0.10.0, go-telegram/bot v1.28.0 and pgx/v5 v5.12.0. These module results supersede the earlier GitHub-page observations below; recheck again when implementing.

| Component | Observed upstream evidence | Port requirement |
| --- | --- | --- |
| OpenWA gateway | [Latest release currently redirects to v0.29.1](https://github.com/rmyndharis/OpenWA/releases/latest) | Recheck gateway release and engine compatibility |
| OpenWA Go SDK | [Module](https://github.com/rmyndharis/OpenWA/blob/main/sdk/go/go.mod) is github.com/rmyndharis/OpenWA/sdk/go, requiring Go 1.22 in the inspected main version | Resolve its own latest module release; do not infer SDK version from gateway tag |
| Telegram client | [Latest release currently redirects to v1.27.0](https://github.com/go-telegram/bot/releases/latest); [release README](https://github.com/go-telegram/bot/blob/v1.27.0/README.md) advertises Bot API 10.3 | Recheck latest release and local Bot API compatibility |

## Preliminary coverage

The [OpenWA SDK documentation](https://github.com/rmyndharis/OpenWA/tree/main/sdk/go) exposes typed clients, injectable transport and optional retries. Its [message client](https://github.com/rmyndharis/OpenWA/blob/main/sdk/go/messages.go) includes send, reply, edit, reaction, history, poll vote and stored media operations. These are main-branch observations, not verified released SDK coverage.

The [Telegram client documentation](https://github.com/go-telegram/bot/blob/v1.27.0/README.md) supports custom API server URLs, webhook secret handling and uploads. Verify all required managed-bot, forum, native-time and reaction features against actual released types and the selected local server.

| Capability | Release-level verification required before Go implementation |
| --- | --- |
| Routing and controls | Managed bot creation/token/access APIs; group picker rights; callback binding; custom local server and durable webhook admission |
| Sends and edits | Exact quote/mention/caption edit wire formats, including explicit empty-caption serialization and released engine support; per-send idempotency header injection; no implicit ambiguous mutation retries |
| Media/history | Bounded streaming source download, stored miss classification, deep-history response metadata and restartable media references |
| Reactions and deletion | Custom emoji lookup, native reaction limits, actor bot permissions and idempotent delete/update handling |
| Status and metadata | Passive authenticated Status retrieval, no view receipts, complete roster aliases and unavailable-field semantics |

No verified SDK gap has yet been established. If a required operation is absent from the latest release, record the gap and obtain a behavior/workaround decision; do not silently switch libraries, use unreleased main or remove the feature.

PostgreSQL and Compose requirements are explicit owner decisions. Their implementation versions, database schema, driver and cutover procedure are deferred to port design; no existing SQLite deployment is converted by these docs.
