# Conversation owner commands

`internal/control.Operator` implements `/keep`, `/mute`, `/ignore` and `/read` through authenticated admission, the native dispatcher and fenced worker in isolated fixtures. It uses the existing Go SDK clients and command/reply journal. [Start help and command menus](STARTUP-CONTROLS.md) now have tested capabilities. Bot creation, mirror transitions, other owner commands, Status author controls and final runtime composition remain pending.

Commands follow the existing addressing rules: unqualified group commands belong to the master; a member bot handles commands addressed to its own username. Other recipients and unauthorized actors cannot queue actions. Caption commands work; command and former-command edits never execute or relay. Arguments to these four commands are intentionally ignored, matching the legacy implementation.

Group commands resolve their exact management topic or whole-group mirror. A member-bot private command resolves its existing bound conversation through the verified identity directory, retaining an established LID route when a phone alias is learned. It neither creates a missing direct route nor requires a new Start when a route already exists. An unknown context receives the original guidance: “Use this command inside a linked conversation or its management topic.” A mapped reply belonging to another conversation is rejected before action admission. Private message IDs remain bot-scoped.

## Preference and reply persistence

Source command recording, its expiry timer, its control plan and the independent management job commit together. The original ingress observation remains durable if preparation fails; partial action admission and timers roll back. The source update does not apply the preference or call WhatsApp.

Management execution takes the shared account-directory lock and route lock. It commits the selected mode and a complete confirmation plan in one transaction. Provider calls run after that commit. The existing [PostgreSQL locking rules](https://www.postgresql.org/docs/18/explicit-locking.html#LOCKING-ROWS) serialize these short state transactions with binding and route changes. No new migration is needed: jobs and control plans already provide the durable journal.

The exact legacy modes are preserved:

| Command | Result |
| --- | --- |
| `/mute` | `muted` |
| `/ignore` | `ignored` |
| `/keep` while mode is `group` | `group` |
| `/keep` in any other mode | `inbox` |

Destinations, inbox-retirement flags, bindings, aliases and mappings remain intact. Preference confirmations come from the master bot at the route's stored Telegram chat/thread, including when a command originated in a member bot's DM. Their exact text remains “Conversation stays here.”, “Conversation muted. Use /keep to resume.” or “Conversation ignored. Use /keep to resume.” Commands and notices use the existing durable 60-second default expiry.

Once prepared, a management retry reuses the original notice target, text, TTL and effect key. It does not apply its mode again over a later owner decision or route move. A local recording failure after a confirmed receipt reconciles that receipt without another send. An ambiguous notice send retains the normal uncertain state and explicit review requirement; later independently queued owner decisions can still proceed. Closing the original database pool and constructing a new consumer preserves this recovery behavior.

## Explicit reads

`/read` queues the existing independent `mark_read` workflow for the resolved WhatsApp conversation, including when that conversation is muted. It sends no Telegram confirmation and does not change the preference. Native `Chats.MarkRead` receives the frozen chat ID; a provider refusal/retry does not repeat a WhatsApp text send. Ordinary incoming relays and preference commands still do not mark WhatsApp read. Engine-specific read scope remains the [provider API's behavior](https://github.com/rmyndharis/OpenWA/blob/main/docs/06-api-specification.md), as in the existing adapter; this slice does not change the reviewed gateway pin.

These components are exercised only with disposable PostgreSQL and native SDK loopback fixtures. `serve` still needs the remaining workflows, managed credentials and validated legacy importer before worker startup and live cutover.
