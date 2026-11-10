# Identity and mention components

`internal/identity` keeps provider-verified aliases, contact names and bot bindings separate. `internal/mentions` resolves incoming native Telegram entities and outgoing WhatsApp tags before the text relay saves its complete delivery plan. These components are connected through `relay.Options.Mentions`; `serve` still needs the event dispatcher and remaining workflows before it can run the bridge.

## Identity evidence

Device suffixes normalize away and `@s.whatsapp.net` normalizes to `@c.us`. An `@lid` remains a privacy identifier until independently verified provider evidence establishes its phone identity. Neither contact display numbers nor group participant `number` fields establish that mapping. The native OpenWA phone adapter verifies the returned contact ID and validates phone digits; an unresolved phone remains unknown.

PostgreSQL stores aliases by permanent account. Writes require an active fenced job. An existing alias cannot change identity, and a new alias cannot silently merge two separately bound bots. Alias writes and contact binding share an account lock so concurrent operations cannot bypass that check. Conflicts remain definite local refusals for manual review. No alias operation rewrites routes, mappings, stable send keys or receipts; legacy reconciliation belongs to the explicit importer and future identity workflow.

Directory reads use one consistent database snapshot. Device placeholders cannot hide a readable cached profile or a registered bot for the same canonical person. Explicitly synchronized profile names survive later webhook placeholders. Contact-bot lookup follows verified aliases; binding a known alias stores its canonical person.

The webhook-to-canonical-message resolver and independent self-identity workflow are still pending. The text capability continues to require verified sender input. In particular, these components do not infer the owner identity from the peer in a private outgoing event.

## Incoming mentions

Only explicit WhatsApp mention IDs authorize replacement of numeric `@` tags. Literal numbers, embedded email-like text and ambiguous numeric IDs stay literal. Aliases allow a tagged LID and its verified phone token to render as the same person.

A registered bot present in the destination group becomes a native Telegram `text_mention`. An absent bot links to its Telegram username; an unbound person links to WhatsApp, using the generic launch link for unknown phone identities. A definitive membership denial uses the link fallback; a transient membership error is propagated. Repeated tags use one lookup per person. Entity offsets remain UTF-16 units through the shared presentation formatter and splitting.

## Outgoing mentions and rosters

Registered bot usernames, native name mentions and copied `@{label|token}` tags resolve to exact WhatsApp IDs. Usernames are case-insensitive. Unregistered usernames remain literal. Code, preformatted text, URLs, text links and email spans are excluded. Invalid UTF-16 entity boundaries fail before delivery.

Copied tokens persist their permanent account, group and person association. Labels are cosmetic and may change after copying. The legacy digest and short-key collision-extension algorithm are preserved. A token from another group/account, an unknown token or a departed member is refused before sending. Alias evidence learned after a token was copied is resolved when preparing its first send.

Group mentions require a roster younger than five minutes. The native group adapter rejects a missing participant list, malformed identities or a different group. Failed refreshes may expose explicitly labeled cached data to a future picker, but never validate an outgoing send. A future-dated cache also requires refresh. Fresh roster persistence is fenced, and an older concurrent response cannot replace a newer snapshot.

Once prepared, incoming rendering and outgoing text/native mention IDs live in the relay plan. Known retries reuse the saved text, entities, destination and idempotency key without rechecking changed bindings or rosters. Already saved plans remain executable if the mention resolver is unavailable; new mention work requires it.

## Verification and remaining integration

Tests port the legacy mention cases for emoji offsets, aliases, placeholders, links, exclusions, copied labels, group validation and retry stability. Real PostgreSQL tests additionally cover immutable alias evidence, account-scoped tokens, expired claims, roster ordering and concurrent alias/bot-binding conflicts. SDK loopback tests exercise roster/phone reads and native sends, including rate-limited retries after bindings, membership and roster availability change.

The `/mention` command, pagination/callback controls, owner-facing refusal notices, captions, edits and history dispatch remain pending. The resolver can supply those handlers with validated tags and cached-roster status; this slice does not install their user interface or start live processing.
