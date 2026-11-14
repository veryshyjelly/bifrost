# Runtime composition

Native deployments can set `BRIDGE_BIND_HOST=127.0.0.1` to restrict the HTTP
listener to loopback. Both `run` and passive `serve` use this optional IP literal;
omitting it preserves the existing wildcard listener used inside Compose.

Optional Termux setup and recovery guidance is documented in
[PHONE-HOST.md](PHONE-HOST.md).

The workflow graph is now composed in `cmd/bifrost/runtime_services.go`: native text/media delivery, commands and callbacks, bot registration/provisioning, route transitions/retirement, mentions, history, metadata/cron, Status, reactions/polls/edits/revocations, notices and cleanup. Construction starts no provider requests, worker or scheduler. Duplicate job-handler registrations fail rather than replace another workflow.

The isolated PostgreSQL fixture exercises the assembled graph through hosted logout, local identity verification, webhook settings, topic creation and a native incoming message. Repeated ingress keeps one copy. Management actions dispatch through their specialized handlers before generic preference commands, including history and contact creation; reports use the native `control-sync-report` kind.

Bootstrap consumption is restricted to bot setup/menu jobs, leaving ordinary ingress pending. Dispatch clients require a confirmed local identity check for the current encrypted credential revision and callback configuration. Cached identities, registration and Start do not establish readiness. Bootstrap refuses failed/uncertain setup instead of replaying unknown logout work. The ordinary worker is started only after webhook setup completes for the current credentials.

PostgreSQL runtime ownership uses a dedicated pooled connection holding a session advisory lock. A second Go runtime for the account is refused. Connection loss invalidates ownership; the lifecycle must stop work before releasing its lock, and an unconfirmed unlock closes the session instead of returning it to the pool. This does not coordinate the legacy TypeScript process: cutover must stop that writer first. [pgx connection lifetime](https://github.com/jackc/pgx/blob/master/pgxpool/conn.go) defines the underlying acquire/release behavior.

Explicit `run` configuration is separate from diagnostic configuration. It requires the permanent identity, provider credentials and owner scope, preserves the legacy history/media/delivery-interval defaults and redacts credentials when formatted. Diagnostic `serve` remains non-dispatching.

`run` holds account ownership throughout startup and shutdown. It exposes authenticated durable ingress while setup buffers ordinary work, checks the OpenWA session and retained self identity, completes bot migration/setup, then rechecks each native local bot and refreshes its webhook without dropping updates. Restart preserves the original master ciphertext and refuses a changed token. OpenWA setup owns the exact callback URL, clears filters/extra headers, restores signed delivery of the supported events and refuses duplicate ownership. Unrelated subscriptions are preserved.

Only after setup succeeds does the runtime start its single fenced worker, persistent metadata cron and durable Status startup refresh. Queue admissions preserve `DELIVERY_INTERVAL_MS` pacing; no wait consumes a job lease or provider attempt. Notice expiry already uses durable scheduled jobs. `/readyz` returns 503 during setup/shutdown and 200 after configuration; `/healthz` remains liveness. Lost PostgreSQL ownership cancels the worker/scheduler, joins them and shuts down ingress before releasing its session.

The isolated native fixture tests signed ingress buffering, duplicate delivery, restart, unchanged credentials, local verification on every restart, second-owner refusal and termination after its ownership connection is killed. The asset activation CLI and deployment rehearsal are described in [LEGACY-IMPORT.md](LEGACY-IMPORT.md). See [CUTOVER.md](CUTOVER.md) for host-transfer precautions.
