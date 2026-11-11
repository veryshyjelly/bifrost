# Telegram notice expiry and manual clearing

`internal/control.Cleaner` implements durable notice expiry and `/clear`, `/clear log`, `/clear_log`, `/clear all` and `/clear_all`. Its command handler and background job handlers connect through the dispatcher registry. They are tested components; complete runtime composition, credentials and legacy import still precede live cutover.

## Durable expiry

Recording a command or confirmed control reply schedules its expiry job in the same PostgreSQL transaction. The existing deadlines remain 60 seconds for commands/plain notices and 600 seconds for interactive controls unless configured otherwise. Jobs retain their deadline and deduplication key across restart; migration `00009_cleanup.sql` also backfills previously recorded timed notices. Zero-expiry archives, ordinary chat messages, service messages and protected messages do not acquire automatic deletion timers.

An expiry job rechecks the recorded scope, current classification, deletion marker and deadline before calling Telegram. Already removed or protected records become no-ops. A changed future deadline defers execution. Groups use the master bot for deletion; private messages retain their bot scope, so identical message IDs in different private bots cannot collide. Definitive 400/403 refusals retain the message and record a safe cleanup problem instead of claiming deletion.

## Fixed manual-clear snapshots

`/clear` defaults to notices, as do `/clear log` and `/clear_log`. These select only recorded log/command messages in the invoking chat and effective topic. They preserve conversation messages, bindings and mappings. `/clear all` and `/clear_all` additionally select recorded conversation/service messages, but only through the master in the configured management group. General and Status topics use the same exact-topic rule. Mirrors, private chats and member bots support notice clearing only.

The clear command atomically records a run, its cutoff and explicit observation membership before sending the original background-start notice. Subsequent messages and notices, including the start notice and completion summary, remain outside that snapshot. Explicit membership also excludes an older-sequence row that commits after selection. PostgreSQL gives each Read Committed statement its own committed-data snapshot; relying only on a sequence cutoff would not provide this guarantee. See [PostgreSQL transaction isolation](https://www.postgresql.org/docs/18/transaction-iso.html).

Background steps process at most 100 recorded rows. Messages at least 48 hours old and protected creation messages are counted as skipped. A bulk 400 switches the run to individual deletion; a refused individual item is counted as blocked and later items continue. A 403 stops the run with the original Delete Messages permission notice. Replies retain the original requested bot/chat/topic, while group deletions use master permissions. Completion counts and the next continuation job commit together.

Notice-only cleanup also protects relay copies whose local mapping write is delayed. An ambiguous own-bot log waits while a matching relay send is sending/uncertain; a confirmed relay receipt excludes that copy even before its mapping exists. Clearing an archive copy marks its observation deleted without erasing its mapping or provider receipt. Quote lookup excludes that deleted copy, and future edit/history handlers must retain the same no-resurrection rule.

## Separate idempotent deletion journal

Telegram's [deleteMessages](https://core.telegram.org/bots/api#deletemessages) accepts 1–100 IDs and skips missing messages. The cleaner therefore stores frozen deletion targets and outcomes in `cleanup_steps`, separately from uncertain message-send effects. A lost response or failed local outcome commit can retry the same IDs safely; a committed outcome is not called again merely because job completion retries. Normal message sends and cleanup summary/permission replies keep the existing effect ledger and uncertain-send review.

Each step verifies its PostgreSQL claim and checks the worker's local lease before a provider call. Provider calls occur outside transactions. Cursor/count changes, deletion markers, refusal diagnostics and continuation admission remain fenced and atomic. No cleanup handler sends to WhatsApp, marks WhatsApp content read or changes contact bindings/routes.

## Verification and remaining work

Real isolated PostgreSQL tests combine authenticated commands, the worker and the native Telegram SDK against loopback providers. They cover recreated expiry consumers over a reopened database, deadline backfill, response loss, local commit failure, scope isolation, fixed membership under late commits, batch fallback, permission notices, relay receipt gaps and retained mappings/deletion markers. SDK tests verify bounded native batches and read-safe deletion failure classification.

The legacy importer must still preserve existing observation classifications, dates, deadlines, mappings, unfinished cleanup runs and keys. [Former-inbox retirement](INBOX-RETIREMENT.md) now has its own fenced handler and reservation journal. Other owner commands, menus, media/edit/history workflows and final runtime composition remain pending. These checks do not delete live messages or change the existing deployment.
