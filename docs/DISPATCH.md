# Source normalization and dispatcher

`internal/bridge.Dispatcher` connects authenticated durable events to the text relay and independent read receipts through the fenced worker's handler registry. It also exposes explicit extension points for remaining WhatsApp, Telegram and management workflows. Constructing the dispatcher does not start a worker or install callbacks. `serve` still needs complete workflow composition and legacy import before replacing the live bridge.

## Admission and stable conversations

For complete native WhatsApp message events, `Store.Admit` commits the original job, an immutable source snapshot, verified alias evidence and contact metadata together. The snapshot retains the raw provider chat address, stable conversation key, sender, linked identity, display name, body, quote and explicit mention IDs. The original payload remains unchanged. Duplicate webhook keys cannot rewrite the snapshot or profiles.

Device suffixes and phone JID domains normalize through the shared identity directory. Only inbound `senderPhone` evidence may associate a LID with a phone. Contact display numbers and own-event `senderPhone` fields never establish that mapping. Conflicting evidence rolls back all tentative identity changes while retaining the original event for definite worker failure/review; a database failure withholds acknowledgement.

A conversation established under a LID retains its existing key when a verified phone alias appears. Later phone and LID events enter that same ordered stream before claims. Existing routes, mappings, source snapshots, job IDs and stable delivery keys remain intact. If both identities already have independently established conversation state, the bridge requires reconciliation rather than choosing a history or deleting a topic automatically.

Contact-bot binding, the queued inbox-retirement action and private outgoing route lookup use this same stable key. Binding a bot through a learned phone alias does not create a second route or lose quotes from private copies. The independent [inbox retirement handler](INBOX-RETIREMENT.md) now deletes eligible former topics through the worker in isolated fixtures.

The lower-level `Store.Enqueue` does not normalize provider events; provider ingress uses `Admit`. Work already queued without a snapshot has a fenced recovery path. It freezes source identity before any effect, preserves the original job ID/key and moves the job to the resolved stream. If an earlier or active conversation job prevents execution, it releases the old claim and requeues without consuming an attempt. Claims also exclude any active job in their stream, including a newer active job encountered during recovery. Stream changes and claims share a short PostgreSQL topology lock; no provider call runs under it.

An unsnapshotted job with existing effects requires explicit import/reconciliation. Normalization never guesses a new destination for work that may already have sent.

## Native event dispatch

Native WhatsApp text events use the persisted source snapshot, then the existing route/mention/text plan. The dispatcher fetches the native group name only when creating an initial group inbox. Muted/ignored sources and known echoes do not require that lookup. Incoming relays do not mark WhatsApp messages read.

Phone-originated owner messages use independently resolved self identity from the configured session's native response. A verified phone is pinned to the permanent account; conflicting replacement evidence is refused. An unknown phone uses the generic WhatsApp launch link. The peer's number is never used for the owner link. Saved plans and known echoes bypass fresh self resolution. Session lifecycle refresh/review controls still need their dedicated handlers.

Telegram dispatch parses the native update and rechecks the owner against both native actor fields and exact JSON keys. Commands, edits, callbacks, managed-bot events and non-text/combined content go to a dedicated extension handler. They cannot be forwarded as a partial plain-text message. Ordinary child-bot group suppression and exact private/topic reply isolation remain enforced in the text capability. Management read jobs invoke the existing independently retried read handler.

If a dedicated handler is absent, the worker retains the unsupported job in its failed/review state. The dispatcher does not acknowledge it as completed or send only its caption. Remaining handlers must be installed before runtime startup; this is an implementation registry, not a partial live mode.

The mention-picker extension now handles `/mention`, its scoped pagination callbacks and temporary mention-validation refusal notices. Refusal plans resume before reattempting outgoing validation, so an already refused message cannot become a send on notice retry. See MENTION-PICKER.md for callback guards and expiry metadata.

## Validation and remaining work

The [metadata run coordinator](METADATA-RUNS.md) composes the [snapshot/text stages](METADATA.md), [picture stages](METADATA-PICTURES.md) and [permission advice/sealed reports](METADATA-ADVICE.md). Isolated fixtures route native owner `/sync` through management admission, `sync-start`, source-scoped `sync-target`, waiting `sync-seal` and `control-sync-report` jobs. Shared contacts deduplicate, known partial outcomes retain warnings, and unresolved sends block finalization. Persistent configured-gated cron supplies the same admission path. These handlers and the scheduler await final runtime composition; `serve` still does not dispatch automatically.

Isolated PostgreSQL tests exercise authenticated HTTP admission, normalization, the worker, dispatcher and both real SDKs against loopback providers. They verify native mentions, quoted replies, stable mappings, independent read receipts, echo suppression, native group titles and independently verified owner links. Recovery tests cover phone/LID stream continuity, older moved jobs behind active jobs, stale claim rejection, unchanged retry budgets and atomic source/alias rollback. Source profiles and prepared requests remain stable across duplicate admission and retries.

The registry also accepts [native group selection and topic headers/actions](GROUP-PICKER.md), [mirror commands and transition jobs](MIRROR-TRANSITIONS.md), [Start help and migration-gated menus](STARTUP-CONTROLS.md), [conversation preferences and explicit read commands](OWNER-COMMANDS.md), the [notice expiry and scoped clear handlers](TELEGRAM-CLEANUP.md) and [former-inbox retirement](INBOX-RETIREMENT.md), including internal `control-` continuations. Other command handlers, media/structured messages, edits, reactions, WhatsApp deletion/repair, lifecycle notifications and managed-bot credentials remain pending, together with the explicit legacy importer and final runtime composition. No live service, callback, paired session or provider message is changed by these fixture tests.
