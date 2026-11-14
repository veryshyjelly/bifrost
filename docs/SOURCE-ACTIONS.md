# Explicit source actions

Owner `/react` and `/delete` commands and authenticated management source actions are implemented through isolated native SDK/PostgreSQL fixtures. Explicit `run` composes these capabilities after native identity and callback verification; diagnostic `serve` remains passive.

Telegram commands require the owner, correct bot addressing, a linked context and a mapped reply in that conversation/topic/private bot scope. Edited commands stay passive. A missing reply produces the existing temporary guidance. `/react` uses its first argument and an empty argument removes the source reaction, preserving the baseline syntax.

The command observation/expiry, frozen request and independent action job commit together. Command replay cannot create another action. Authenticated management `operator_action` events retain their direct `chatId`, `message` and `emoji` surface; their accepted action is likewise reused on replay. Requests serialize by source message and retain their provider session, exact message target, operation, emoji and notice destination.

Source deletion always passes the native SDK's explicit `forEveryone: true`. A gateway acknowledgement completes only that provider operation. It does not invent an authoritative revoke or delete Telegram archive content: some engines can fall back to deleting for the linked account when an everyone-revoke is unavailable. An actual source revoke follows the separate [revocation-choice workflow](REVOCATION-CHOICES.md).

Native reaction and deletion calls use a frozen idempotent journal. A lost response or crash can repeat the same operation without becoming an uncertain message send. Definitive permission/availability/unsupported refusals preserve Telegram content and queue an independent temporary notice; the next action can proceed. Unresolved transient failures retain their request and normal retry/review state.

Migration 26 stores source jobs and their unique originating command/management job. Fixtures cover mapped-reply admission, edited commands, exact native reaction/removal/delete requests, command and management replay, refusal without archive mutation, and recovery after provider acceptance. Route repair and remaining operational workflows are tracked in [RUNTIME.md](RUNTIME.md).
