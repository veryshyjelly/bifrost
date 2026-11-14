# Operational commands and notices

`/status` freezes the existing diagnostic shape: configured state, member bot and conversation counts, metadata cron/timezone/latest completed report, control lifetimes/cleanup problems and ordered queue-state counts. It exposes no credentials, provider URLs or raw source payloads. Reply retries preserve the original snapshot, destination, command observation and expiry.

Master `/inbox` reads the invoking chat with native `getChat`, validates its exact ID and checks `IsForum` against the configured management chat. It preserves the original connected/configuration guidance and never changes configuration. An already prepared reply reuses the frozen result rather than repeating the read. Owner, private-chat, addressed-member and ignored edited-command rules match other controls.

Session status/disconnection and `message.failed` events validate their original account/session/idempotency envelope and admit independent `session_status` management notices. Missing native status uses `changed`. Authenticated management `session_status` and `delivery_problem` actions render literal text in management General with the usual independent notice expiry.

The future runtime composition must explicitly call `ConfigureOperationNotices`. Diagnostic `serve` does not enable it or start a worker. With that setting present, failure/retry/review notice admission belongs to the fenced job-state transaction. A failed outbox insert rolls back the source state change. Recovered unknown effects get review notices while retaining their uncertainty. Job/attempt keys deduplicate admission, and separate management streams keep a failed source from blocking its report. Safe state-derived reasons avoid provider descriptions containing private data.

Control jobs, existing `n8n` jobs and management status/problem notices do not generate delivery-problem notices, preventing failure recursion. Completed jobs generate none. Explicit `run` composes these capabilities after native identity and callback verification; diagnostic `serve` remains passive.

Native SDK/PostgreSQL fixtures cover frozen status snapshots, latest-sync field compatibility, command expiry, forum checks and retries, passive native session events, atomic notice rollback, recursion suppression, disabled diagnostic stores and recovered uncertain effects.
