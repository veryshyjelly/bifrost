# Durable metadata runs and scheduling

`internal/metadata.Coordinator` composes the existing snapshot/text, picture and advisory stages into a complete metadata run. Isolated native ingress/dispatcher/worker fixtures exercise owner `/sync` through admission, target discovery, refresh and completion delivery. The coordinator and scheduler are available for runtime composition; `serve` remains diagnostic/opt-in ingress until the remaining workflows and existing import are ready.

## Admission and discovery

A claimed `management` job containing `sync_metadata` admits a run under the account directory lock. Admission, the initial request membership and `sync-start` job commit together. Replaying that request reuses its original run even after completion. Requests coalesce with the oldest active run in the same account/session when that run covers all conversations or the same canonical scoped target. A coalesced manual request with a notifier joins the run's waiters; the original command still supplies its notifier or management fallback.

Global discovery includes linked group routes except muted/ignored routes, plus every registered contact-bot binding, including contacts without direct routes. Scoped work selects its group or contact. Frozen group rosters fan out to existing bound contact bots before group updates. Canonical aliases and the destination bot deduplicate shared contact work across groups. Refresh provisions neither a bot nor a direct route.

Each target runs through the existing fenced stages. Metadata jobs use separate ordered source streams scoped to session and group/contact bot. A predecessor check also considers current verified aliases and bot bindings. An uncertain upload remains a barrier to subsequent work for that source; unrelated chat streams retain their priority and can continue. Waiting on predecessor/finalization work releases the claim durably without spending a provider retry attempt.

## Outcomes and recovery

Known text-field refusals are recorded atomically with the discarded effect. Remaining fields continue, while refused topic names never become applied-name evidence. Transient provider failures retry for up to four target attempts. A definite failure or exhausted provider retry records a warning outcome with confirmed partial work and available advice. Local persistence failures and lost claims remain recoverable local errors. Sending/uncertain effects never become completed warning outcomes or automatic retries.

Snapshots, plans, converted picture bytes, field refusals, native mutation/observation receipts and final outcomes survive a new database consumer. Failure to record the outcome after confirmed mutations retries that local write using saved stage evidence, without downloading or sending the updates again. Summary counts come from confirmed mutation receipts; partial targets retain completed unavailable-picture counts, roster-change evidence and frozen advisory results.

The sealing job waits for every discovered target to have a terminal outcome and a completed worker job, and for the original request jobs to finish. It then commits `control-sync-report` membership and the completed run state together. Report delivery retains its own receipts and uncertainty handling; completing refresh work does not assert that every notifier has received the report. See [metadata advice and reports](METADATA-ADVICE.md) for notifier, quiet-run and button rules.

## Cron

`SYNC_CRON` defaults to `0 * * * *`, with `SYNC_TIMEZONE=Asia/Kolkata`; `off` disables scheduling. Compose and `make configure` expose these settings. The numeric five-field grammar supports lists, ranges and positive steps, Sunday `0`/`7`, and the baseline day-of-month/day-of-week rules. Named fields, macros and invalid timezones are rejected. The pinned `github.com/robfig/cron/v3 v3.0.1` parser supplies timezone/DST scheduling after explicit baseline grammar normalization; embedded timezone data supports the slim runtime image.

`Coordinator.Tick` admits only a matching minute while the runtime is configured. `Coordinator.Schedule` checks the current minute and then wakes at minute boundaries; missed minutes are not replayed. PostgreSQL records each account/session/raw-expression/UTC-minute once, atomically with admission, so concurrent schedulers and process restarts cannot duplicate it. Timezone is intentionally excluded from that key to retain the original cron identity. Scheduled work joins an existing global run when available. Unchanged cron runs stay quiet for management while coalesced manual waiters still hear the result. The loop is not started by the current diagnostic `serve` command.

Migration `00017_metadata_runs.sql` adds durable runs, admissions, request/target membership, outcomes and cron minutes. Import/recovery must retain them together with metadata plans and receipts. No automatic pruning or live cutover is introduced.

Migration `00030_metadata_trigger_sources.sql` preserves the original `new`, `link` and `bind` report sources alongside `command` and `cron`. Automatic producers use an internal `sync-request` admission, with their original dedup keys. Public management requests retain the baseline `command` source even if their payload supplies a different label. Binding scope is contact-only and linking is group-only; internal requests cannot masquerade as quiet cron work. Coalescing keeps the first run's source. Every non-cron run retains its initial notifier/default management report plus explicit manual waiters; existing frozen runs and reports are never relabelled.
