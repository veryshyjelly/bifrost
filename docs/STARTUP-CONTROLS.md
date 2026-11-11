# Start help and Telegram command menus

`internal/control.Starter` handles ordinary `/start` commands through authenticated ingress, the dispatcher and fenced control-reply journal in isolated fixtures. `internal/control.Menus` installs the existing command menus through native SDK settings calls and a separate idempotent journal. Neither component starts the runtime worker or changes live callbacks. Mirror/sync deep links, other owner workflows, credential migration/import and final runtime composition remain pending.

## Start behavior

The original master private/group help text and member private/group guidance are preserved. Unqualified group commands belong to the master; a member handles a command addressed to its own username. Unauthorized actors, other recipients, command edits and former-command edits cannot activate a bot or relay text. Caption commands use the shared command parser.

A member's private Start marks the registered bot started even before binding, matching the existing behavior. An unbound bot sends the original binding guidance. A bound bot sends the original ready/history guidance, creates a missing direct route without a forum topic and queues independent former-inbox retirement. Existing routes, paused modes and messages remain intact until that separate cleanup executes. Verified aliases retain the established conversation key. Group Start never activates private routing; it offers the original private-chat button when the recorded username is available.

Activation, direct-route admission, cleanup admission, source command expiry and the entire help plan commit together under the account-directory lock and current job claim. A local plan failure rolls everything back while retaining the original ingress. Once prepared, retry reuses the original help and effects: binding an initially unbound bot later does not change the older reply or create routes from its retry. Uncertain message sends retain the normal review rules.

Commands and ordinary help notices use the existing 60-second default expiry; a group member's private-chat button uses the existing 600-second interactive expiry. Master private `mirror_…` links and master group `sync_…` links are delegated to their dedicated future workflows. They are retained as unsupported work when no such handler is installed, rather than consumed as ordinary help.

## Migration eligibility and menu admission

Migration `00011_telegram_menus.sql` stores explicit bot lifecycle records and per-job menu plans. `Store.RecordBotMigration` is a fenced persistence boundary for an already completed migration or validated import. It validates the native bot identity and refuses identity replacement; it does not migrate a bot, retrieve credentials, log out a hosted API session or install a webhook. Those workflows still need implementation. Registration and Start alone never establish local API migration eligibility.

Future runtime composition calls `Store.QueueMenus` with the permanent account and a fresh startup ID. Only explicitly migrated master/member bots are eligible; members must also remain registered. Reusing a startup ID deduplicates admission. Each bot has an ordered menu stream so a pending older installation cannot overwrite a later one. Queue admission is transactional. The importer must preserve validated migration markers and unfinished menu plans before enabling this path.

Menus preserve the exact legacy command order/descriptions. Master default commands include the existing routing, Status and owner actions; member default commands retain the original common list. The master additionally installs `inbox`, `link` and `clear_all` for the configured owner in the management chat through a native `chat_member` command scope. All bots receive the default `commands` menu button. Entries for pending workflows are retained for parity; only complete runtime composition may install these menus live. The golden fixture in `internal/control/testdata/legacy-menus.json` was generated from checkpoint `checkpoint-typescript-baseline`.

## Settings recovery

Each installation reads native `getMe`, validates the identity against its migration marker and freezes the owner, management chat, ordered commands and identity before setting anything. It records confirmed progress after default commands, menu button and the master's owner-scoped commands. The installed timestamp commits only with the final outcome.

The current [Go SDK](https://github.com/go-telegram/bot/blob/v1.28.0/methods_params.go#L790) exposes native `SetMyCommandsParams` and `SetChatMenuButtonParams`; scopes retain full-width integer IDs and the [menu button](https://github.com/go-telegram/bot/blob/v1.28.0/models/menu_button.go#L74) explicitly supplies its `commands` type. Telegram's [command scope](https://core.telegram.org/bots/api#botcommandscopechatmember) limits the additional management menu to that owner; command execution still enforces its own authorization. Master identity metadata stays in lifecycle state and does not add a member-bot registration.

These settings overwrite the same frozen values. Native adapters classify lost responses/transient failures as safe to retry, and settings use their own fenced journal instead of message-send effects. Each call checks the local worker lease. Failed local progress commits can repeat only the same settings; recorded steps skip the API. A new consumer over a reopened database resumes the original scope even if current configuration changes. Identity mismatch and missing migration eligibility fail before settings calls. Ordinary sends keep their existing uncertainty handling.

Fixtures cover native serialization, ordered menu parity, migration selection, admission deduplication, Start rollback and cleanup, addressing, expiry, lost settings responses, partial completion, database reopening, final-timestamp rollback and stale/cross-account fencing. [Manual mirror transitions](MIRROR-TRANSITIONS.md) now have tested components. Complete runtime startup, `/status`, private group selection, other controls and legacy import remain pending.
