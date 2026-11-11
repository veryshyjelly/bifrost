# Mention picker and owner replies

`internal/mentions.Picker` supplies the dispatcher's mention-picker and mention-refusal handlers. It uses `internal/control` for frozen Telegram replies, native callback acknowledgements and scoped actions. The handlers are exercised through authenticated ingress, PostgreSQL and the fenced worker; `serve` still requires the remaining workflows and importer before live startup.

## Picker behavior

`/mention [name or number]` works in an active WhatsApp group's management topic or Telegram mirror. Unqualified group commands belong to the master. A member bot handles commands explicitly addressed to its own username; commands for another bot are ignored. Commands can appear in media captions, but edited commands and recorded former commands are ignored rather than forwarded.

The picker searches names, exact WhatsApp IDs and registered bot usernames, then displays ten members per page. Names use the original locale collation and Unicode case conversion. The Go implementation uses pinned `golang.org/x/text` v0.44.0. Native copy buttons copy a registered bot username or the existing stable group/person token. Labels and masked ID previews preserve the original presentation; untrusted text has no parse mode. Golden fixtures generated from the checkpoint's original `Mentions.page` under Node compare complete text, UTF-16 entities and keyboards, including Unicode search and accented-name ordering.

A stale roster may be shown only with the original cached-results label. Sending a copied tag still requires fresh membership validation. A failed refresh without cached data produces the original temporary error notice. The picker, pagination and refusal notices never send to WhatsApp or mark messages read.

## Scoped actions and retries

Navigation actions persist their permanent account, owner, bot, Telegram chat, effective topic, WhatsApp conversation, search, target page and expiry. Every new click validates those fields. Whole-group mirrors ignore incidental Telegram reply-thread IDs. Unknown, expired, muted, moved, wrong-bot, wrong-topic and inaccessible-message callbacks receive the original expired-picker acknowledgement without creating a page. Native owner checks also apply before handlers execute.

The full reply plan, native entities, copy buttons and newly generated actions commit before the first provider mutation. Known retries reuse the original requests and action IDs/deadlines, even if roster or bot metadata changes. Already completed acknowledgement and message effects are not repeated. Telegram's definite expired-query HTTP 400 closes only that acknowledgement in `discarded` state; it does not claim provider success or block an otherwise valid prepared page. Other provider errors retain their normal retry/uncertainty handling.

Replies are observed only from confirmed effect receipts. A local tracking failure after sending repairs the observation on retry without sending another message. Observations are scoped by bot in private chats and shared by chat in groups, so overlapping private reply IDs remain distinct.

## Refusal notices and expiry metadata

Invalid, wrong-group/account, departed-member and unavailable-roster sends produce the original owner-facing mention notices before any WhatsApp send effect exists. The notice uses the legacy `:mention-error:text:part` delivery key. Its plan freezes the refusal: a retry cannot become a WhatsApp send after tokens, bindings or roster data change. These handlers do not convert provider failures or uncertain sends into validation notices. The owner's original conversation message remains recorded as chat content with no automatic-expiry deadline.

`COMMAND_TTL_SECONDS` defaults to 60; `CONTROL_TTL_SECONDS` defaults to 600. Both retain the legacy 5–86400-second range and are available through configuration and Compose. Migration `00008_mention_controls.sql` adds control plans/actions and durable observation expiry metadata. Commands and plain notices receive command lifetimes; picker replies receive control lifetimes. Retrying a recorded reply does not extend its cleanup deadline. Existing conversation, protected and deleted observations are preserved.

The [cleanup handlers](TELEGRAM-CLEANUP.md) now schedule and execute durable notice expiry and scoped manual clearing in the worker fixtures. General command menus, other owner commands and full runtime composition remain pending. Mention rendering for attachment captions, edits and history remains part of those pending delivery handlers.
