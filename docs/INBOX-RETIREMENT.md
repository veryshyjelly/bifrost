# Former inbox retirement

`internal/conversation.Retirer` handles the existing `control-retire-inbox` job through the dispatcher registry in isolated integration tests. Starting a bound bot already commits activation and this independent job together. Delivery to the started bot does not wait for deletion. Mirror linking, `/start` command composition and `/unmirror` still need their full owner workflows before runtime startup.

The approved deletion behavior is unchanged: an active mirror outside management, or a dedicated route with a started bound bot, retires its former management inboxes. Candidate topics come from the current route, remembered empty inboxes and mapped management copies. Initial topic creation now records its inbox in the same fenced transaction as the route. Future link/import code must preserve former inbox records before changing destinations.

General (thread 0 or 1), mirror messages, private-bot copies and other management topics are never deleted by this handler. A former topic containing another conversation's route, mapping or prepared relay is refused. The whole candidate set is checked transactionally, so a shared later candidate does not leave earlier reservations committed. Completed mappings and provider receipts remain retained.

## Reservation and recovery

Migration `00010_inbox_retirement.sql` adds remembered inboxes, per-job retirement plans and account/chat/topic reservations. Plan preparation serializes with route creation/configuration and relay-plan admission using the existing account-directory lock. It waits for incomplete relays at a candidate topic, including failed or uncertain ones requiring review. A late committed competing route is observed before any reservation can commit. No database transaction spans a provider call.

Once committed, the plan's management chat and topic IDs are fixed. Reservations prevent new routes or relay plans from targeting an inbox whose deletion may already have happened. New routing writes, including the future importer and mirror transitions, must take the same lock and enforce these reservations. Raw SQL route writes are not an alternative runtime interface.

The native `DeleteForumTopic` adapter uses the master bot and requires a negative numeric chat ID and thread greater than 1. The [Telegram API](https://core.telegram.org/bots/api#deleteforumtopic) deletes the topic and all its messages and requires `can_delete_messages` rights in a supergroup; the [Go SDK method](https://github.com/go-telegram/bot/blob/v1.28.0/methods.go) exposes the native parameters. A definite missing-thread response counts as already removed. Other 400/403 refusals leave the separate job failed and available for explicit retry; rate limits, transient failures and lost responses can repeat the same idempotent target within the normal worker budget.

Deletion targets and outcomes have their own journal. They do not create message-send effects or relax uncertain-send review. Each call checks the worker's local lease; all plan, outcome and route writes also require the current PostgreSQL claim. Failed outcome commits can repeat only the reserved topic. Committed outcomes skip the API on retry, including after reopening the database with a new consumer.

After confirmed deletion, matching group-scope observations are marked deleted and their cleanup errors cleared. Quote and mapping lookup also respects reservations, including copies without an observation. Final completion detaches only the snapshotted management inbox: a later restoration to a fresh topic is preserved. Preferences, mirror destinations, histories, keys and mappings are not removed. A durable ineligible/no-route result stays a no-op on retry; a later activation gets its own queued job.

## Validation boundary

PostgreSQL/SDK fixtures cover bot activation to independent cleanup, empty/mapped former topics, General/private isolation, shared topics, pending relays, competing route transactions, rebinding during the API call, full-width native parameters, permission retry, lost responses, reopened consumers, outcome/completion rollback and stale-claim fencing. Live messages, callbacks, identities and services are unchanged. Explicit `run` composes these capabilities after native identity and callback verification; diagnostic `serve` remains passive.