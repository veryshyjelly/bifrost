# Source revocation choices

Source revocations are implemented through isolated native SDK/PostgreSQL worker fixtures. Explicit `/delete` and `/react` commands, route repair and complete runtime/import wiring remain in [RUNTIME.md](RUNTIME.md).

A source revoke atomically records a permanent account-scoped tombstone and destination cleanup tasks. Text, attachment and history delivery consult the tombstone, so an original message arriving later cannot recreate revoked content. Keeping a Telegram archive copy never reverses that source tombstone.

Other-author revocations delete retained mapped parts automatically. Owner messages instead receive one Delete/Keep choice per private bot/chat or group/topic. The primary attachment or first retained copy receives an inline keyboard when possible. If native markup is refused, a durable reply prompt retains the same scoped choices. Its reply requires the original message to remain present. A definitively refused prompt does not replace content or leave an unresolved send effect.

Choices validate owner, permanent account scope, originating bot, exact message/prompt, destination, topic, expiry and pending state. The account scope matches the existing permanent-account digest; replacing the OpenWA connection does not change these Telegram archive controls. A completed Keep cannot be reversed by stale Delete clicks or repeated source events. Expiry keeps the original content and retires only buttons or the fallback prompt.

Delete freezes every part in that selected destination. Each idempotent Telegram delete advances a fenced cursor and queues its next step atomically. A crash after provider acceptance can safely repeat that copy. Known deletion refusals retain the surviving content, count the blocked part and queue the existing partial-cleanup notice. They do not write destructive replacement text. Other destinations retain their independent choices.

Task expiry uses a separate stream so a future deadline cannot block an immediate owner choice. Callback decisions and expiry transitions serialize; cleanup cannot reverse a deletion already accepted. Native markup and delete calls use idempotent progress. Fallback prompt sends retain ordinary frozen effects and uncertain-send review. A refused prompt deletion does not falsely mark its observation deleted.

Migration 25 adds revocation tasks alongside the existing tombstone ledger. Acceptance fixtures cover split-part deletion, revoke-before-original delivery, wrong topic and private bot scope, Keep/repeated conflicts, destination isolation, fallback prompt expiry, missing prompts, provider refusal and delete crash recovery. Diagnostic serve still does not dispatch.
