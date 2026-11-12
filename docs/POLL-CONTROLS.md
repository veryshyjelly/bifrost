# Poll controls

Native WhatsApp poll choices are implemented through isolated SDK/PostgreSQL worker fixtures. Polls keep their exact option texts, including whitespace, and use the maintained SDK's `pollMessageId` request field. The SDK also serializes an empty selection as `[]`, which explicitly clears a vote.

Each callback validates its owner, account/session, originating bot, retained poll message, destination/topic, expiry and current private binding or group route. A duplicate callback reuses its confirmation plan. A valid callback admits an independent vote job; callback acknowledgement does not own the provider mutation.

Vote jobs serialize per source poll. Each computes its toggle or replacement from the last confirmed selection, then freezes its exact request. Multiple-choice clicks toggle individual options; a single-choice click replaces the selection. Confirmation is persisted only after provider acceptance. A definitive refusal preserves the prior selection and queues a scoped temporary notice. This includes engines that return 501 for unsupported vote sending; receiving poll votes remains supported separately.

Votes set complete provider state and use a fenced idempotent journal, like command-menu settings. A lost response or process crash can repeat the frozen selection without becoming an uncertain message send. Later choices wait for the earlier poll job. Ordinary messages and notice delivery use separate streams. Receiving a source vote queues its attributed retained-copy notice without sending WhatsApp read receipts.

Migration 23 adds confirmed poll selections and durable callback-origin vote jobs. Native poll actions remain attached to their original delivery plan. Tests cover rapid queued choices, multi-choice removal, single-choice replacement, wrong topic/message/expiry, callback replay, 501 refusal, frozen transport retries, a crash after provider acceptance and passive source-vote observations. Final runtime composition remains in [PORT-COMPLETION.md](PORT-COMPLETION.md).
