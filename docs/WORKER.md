# Delivery worker and initial conversation routing

`internal/delivery` now provides a runnable worker over the PostgreSQL claim/effect protocol. `internal/conversation` implements the first inbox creation and delivery-target decisions. These are tested components; `serve` still exposes diagnostic/review/opt-in admission endpoints and does not start a worker, register callbacks or forward live messages. Full workflow handlers and the validated legacy importer must exist before cutover.

The [native dispatcher](DISPATCH.md) now connects admitted text events and independent read jobs to this worker in integration tests. Complete source admission uses canonical conversation queues; fenced recovery can requeue older unsnapshotted jobs without changing their IDs, delivery keys or retry budget.

## Execution guarantees

The worker defaults to one concurrent job, preserving the baseline's serialized execution. A bounded pool can execute independent streams; PostgreSQL's stream-head guard still prevents later same-stream jobs from overtaking pending, running, failed or uncertain work. Unregistered job kinds fail closed instead of being marked complete.

Claims have renewable leases and random fencing tokens. Startup and periodic recovery touch expired claims only. A local conservative lease deadline and heartbeat cancellation prevent new effects after ownership is lost. Receipt/state writes are independently checked against the live PostgreSQL claim. The worker never resets another process's live claim.

Each mutation goes through `Effects.Do`. Its request is persisted before invoking the provider. Retries receive the original frozen JSON, and completed effects return their original receipt without calling the provider. Callbacks must use their supplied context and return validated receipts; a panic, malformed receipt, ambiguous provider failure or lost receipt write stops further mutations and retains uncertainty.

| Outcome | Job state |
| --- | --- |
| All work and effects completed | done |
| Known retryable refusal | pending, with exponential delay and the provider's longer Retry-After |
| Known permanent refusal or exhausted eight-attempt budget | failed |
| Unknown or ambiguous mutation outcome | uncertain; explicit review required |
| Claim lost or state write unavailable | retain the stored claim/effects for fenced recovery |

Normal cancellation before a mutation can return work to pending. PostgreSQL refuses that transition if sending/uncertain effects exist, so shutdown cannot turn a possible send into an automatic replay. Waiting for an uncooperative handler is bounded; unfinished claims remain recoverable. Reports contain job IDs, states and fixed reason codes, never claim tokens, requests or raw error descriptions.

## Handled refusals

Some existing workflows catch a definite refusal and send owner guidance instead, such as a refused topic creation during `/unmirror`. `Effects.Discard` explicitly closes a pending effect only after the same execution observed a definite refusal. Its request is retained in a terminal `discarded` state; it is not a successful provider receipt. A retry cannot send that branch again. Sending, completed and uncertain effects cannot be discarded.

Migration `00003_discarded_effects.sql` adds that terminal state. A schema downgrade must not reset discarded mutations to pending; use a validated backup for rollback. Normal failed-job retries still preserve completed receipts and frozen pending requests.

## Initial routing behavior

Initial inbox creation is restricted to the canonical conversation's WhatsApp stream. Unknown conversations create one Telegram forum topic through the native SDK method. A retry after topic success but before route persistence reuses that receipt. The route and its new-conversation management notification commit together, under the worker's claim, without overwriting an existing binding.

A verified contact binding to an already started bot creates a dedicated direct route without creating an unnecessary management topic. A bot cannot bind to two different canonical people. Starting a bound bot persists activation and queues independent inbox-retirement work in one transaction. The [retirement handler](INBOX-RETIREMENT.md) now performs native topic deletion with frozen reservations and shared-topic guards, independently of message delivery.

Target selection preserves the existing distinctions:

- Started contact bots receive private conversations in the owner's DM; unstarted bots fall back to the management topic.
- A group sender's bot is selected only when the master sees it as a member, administrator or creator. Absence or a definitive 400 membership refusal falls back to the master; other failures propagate.
- Phone-originated group messages, Status and forced fallback use the master. A started private bot can still present phone-originated owner messages with owner attribution.
- Muted/ignored conversations suppress delivery before membership lookup.
- A retired management inbox requires explicit restoration before master delivery. Selection cannot accidentally post into the whole management forum.

Migration `00004_conversations.sql` adds route names/modes, bot-start state and unique canonical-contact bindings. Native topic/member adapters preserve full-width Telegram IDs and optional SDK fields. None of these routing operations marks WhatsApp content read, copies history or changes the approved 1,000,000-byte outgoing sticker budget.

Verified phone/LID aliases, formatting, scoped mappings, native text dispatch, notice cleanup and former-inbox retirement now have tested capabilities. Managed-bot token registration, remaining owner commands, full mirror/topic transitions, `/unmirror`, history coordination and final runtime composition are still pending. Canonical identity resolution precedes routing; raw aliases are not silently merged by the storage adapter.

## Validation

Race tests cover safe outcome classification, Retry-After, ignored errors, retained receipts/requests, explicit discard, panic/malformed-success handling, lease-loss cancellation, renewals and bounded shutdown. Real isolated PostgreSQL tests run the worker over partial deliveries, blocked stream heads, review gating and a lost response after receipt commit. SDK loopback tests check native topic/member fields and full-width IDs.

The inbox integration test combines PostgreSQL, the real Telegram SDK and an isolated HTTP fixture: one topic creation survives a local route-save failure and retry, followed by one route and one management notice. Binding/start tests prove bot uniqueness, independent cleanup admission and topic-free delivery eligibility for an already started private bot. No live provider operations are used by these tests.
