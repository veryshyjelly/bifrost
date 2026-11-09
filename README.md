# WhatsApp–Telegram bridge

A personal bridge that reuses OpenWA for WhatsApp and represents contacts with Telegram bots. n8n passes signed management events to a durable SQLite queue. One contact bot represents one person across private chats and mirrored groups; the master supplies the sender's name when a contact bot is unavailable. Only the configured Telegram owner can operate it.

This directory is a standalone Git repository. It contains the bridge, both Docker builds, portable Compose deployment, the importable n8n workflow, configuration templates, tests and CI. WhatsApp sessions, credentials and persistent data are managed separately.

| File | Purpose |
| --- | --- |
| `src/`, `scripts/`, `test/` | Bridge implementation, administration and tests |
| `compose.yaml` | Bridge and local Telegram Bot API; connects to existing n8n/OpenWA networks |
| `deploy/compose.n8n.yaml` | Adds a fresh upstream n8n instance |
| `workflows/management.json` | Importable export of the deployed management workflow |
| `bridge.env.example`, `.env.example` | Application and Compose configuration templates |
| `deployment.lock.json` | Tested Node, n8n, OpenWA and Telegram Bot API versions/commits |
| [Installation](docs/INSTALL.md) | Fresh machine and existing dependency setup |
| [Recovery](docs/RECOVERY.md) | State to back up, restore and verify |
| [Current home deployment](docs/CURRENT-DEPLOYMENT.md) | How this repository relates to the existing running setup |
| [Validation](VALIDATION.md) | Checks actually performed and their limits |

## Start with existing OpenWA and n8n

Use Node **26.13.0**, Git, Docker with Buildx, and Compose **2.30+**. The bridge has no npm dependencies to install.

```sh
npm run configure
cp .env.example .env
git config core.hooksPath .githooks
```

Fill the account fields in the generated private `bridge.env`; set the existing Docker network names in `.env`. Run `npm run configure` again after setting the Telegram application ID/hash to refresh `telegram-api.env`. Existing values and encryption keys are preserved. The generated `n8n.env` is only for the optional standalone n8n deployment.

Create a dedicated master bot with **Bot Management Mode**, start it privately, and add it as administrator to a **private forum supergroup** with Manage Topics and Delete Messages. Set the owner and management chat IDs. `node --env-file=bridge.env scripts/identify.ts` discovers them before the first webhook is installed. Set the existing ready OpenWA session ID and operator-or-higher API key without a chat allowlist. Allow `wa-telegram-bridge` in OpenWA's `SSRF_ALLOWED_HOSTS` and recreate that API service.

Import `workflows/management.json` into n8n and publish it. Its webhook path is `wa-telegram-management`; the HTTP node forwards the complete signed body to `http://wa-telegram-bridge:3210/v1/management`. It needs no n8n credential.

```sh
docker compose config --quiet
docker compose build wa-telegram-bridge telegram-api
docker compose up -d --no-deps wa-telegram-bridge telegram-api
docker compose exec -T wa-telegram-bridge node scripts/setup.ts
```

Setup requires the existing WhatsApp session to be ready, verifies the private forum and permissions, migrates the dedicated master to the local Bot API, verifies the n8n webhook, and installs callbacks. For a new OpenWA/n8n installation, follow [the installation guide](docs/INSTALL.md).

Use the installation and recovery guides at the selected revision. Preserve existing identity, credentials and state when restoring an account.

## Daily use

Reply in an inbox topic to send a WhatsApp message. The topic header includes buttons for **Create bot**, **Keep here**, **Mute**, **Ignore**, **Mark read**, and **Recent history**. In group topics, reply to a person's message with `/create` to request that person's bot.

Follow the creation link, then select the contact in your private chat with the master to confirm the binding. Start the new contact bot for private replies. Add it to mirrored groups where you want that sender identity. If a bot is absent, blocked, not yet started, or cannot be created because of your owned-bot limit, the master continues relaying with the person's name.

Member-bot messages use Telegram's sender identity without repeating that person's name in the text, captions or edits. Hermes includes the person's name when relaying on their behalf. History markers and the owner's “sent from WhatsApp” marker are preserved.

To mirror a WhatsApp group, tap **Create / link Telegram group** in its management topic. In your private chat with Hermes, tap **Create / select Telegram group**, create a private group (or select one you own), and confirm Hermes's administrator permissions. The selected group is linked automatically. This uses Telegram's native picker and requires no separate account login. If your client has no create option in that picker, create the private group first and select it there.

For an older topic, send `/group` inside the topic to get the button. The manual alternative is to create a private Telegram group, add the master as an administrator, then send:

```text
/link <WhatsApp group ID from its inbox topic>
```

Your ordinary messages in that linked group go to the WhatsApp group. Bots cannot create Telegram groups or silently add other bots; use the supplied add-to-group links. A contact bot may be reused in multiple groups. Only the master consumes ordinary group messages, preventing one send per participant bot.

Commands inside conversations:

| Command | Behavior |
| --- | --- |
| `/read` | Mark the WhatsApp conversation read |
| `/history` | Import up to 100 available recent messages without duplicating mapped messages |
| `/create` | Request a contact bot; in groups, reply to that person's message |
| `/group` | Show the native create/select-and-link flow for a WhatsApp group |
| `/sync` | Refresh this conversation's WhatsApp metadata and existing member bots |
| `/sync all` | Refresh every active group and registered contact bot; use privately with Hermes or in the management group |
| `/keep` | Resume or retain this conversation in the current topic/group |
| `/mute`, `/ignore` | Stop relaying new messages until `/keep` |
| `/delete` | Reply to a mapped message to request deletion on WhatsApp, subject to WhatsApp permissions |
| `/react 👍` | Reply to a mapped message to set a WhatsApp reaction; `/react` clears it |
| `/status` | Show counts, queue state, metadata schedule and last sync result |
| `/clear log`, `/clear_log`, `/clear` | Remove recorded bot notices and command messages in the current Telegram chat/topic |
| `/clear all`, `/clear_all` | Remove recorded Telegram messages in management **General** only |

A successful outgoing reply schedules a read receipt. Merely receiving a Telegram relay does not mark WhatsApp read. Phone-originated sends are labeled and do not echo back.

Commands addressed to another bot are ignored by Hermes. A member bot can handle commands explicitly addressed to its own username in a group; unqualified group commands belong to Hermes. Telegram join/leave, title and topic service events are not relayed. Editing a command does not execute it again or send it to WhatsApp.

Commands and bot notices are temporary: they are deleted after **60 seconds**, with **10 minutes** for replies containing buttons or a group picker. Set `COMMAND_TTL_SECONDS` and `CONTROL_TTL_SECONDS` in `bridge.env` to change those lifetimes. Expiration survives restarts. WhatsApp conversation messages and attachments remain in the Telegram archive.

`/clear log` clears notices in its current chat/topic without changing WhatsApp messages, bot bindings or delivery mappings. `/clear all` is available only in management General; other topics and mirrored groups keep their history. The bridge can remove only messages it has recorded, and Telegram normally permits deletion only within 48 hours. The completion notice reports older/protected messages or permission failures. Hermes needs **Delete Messages** in groups to remove the owner's commands and other bots' notices. Existing recorded history is indexed for manual clearing; it is not automatically expired on upgrade.

The master and all registered member bots install their command menus on each configured startup. New bots also receive menus when their webhook is installed. Telegram's `/` autocomplete lists the commands; the extra `/clear_all` menu item is scoped to the management group and still checks for General when executed.

## WhatsApp metadata sync

The bridge checks WhatsApp metadata **hourly, at the start of each hour in Asia/Kolkata**. Its five-field cron scheduler runs inside the bridge service and persists each scheduled run in SQLite. Messaging jobs have priority; overlapping requests join an existing run. The service must be running at the scheduled time. Linking a group, receiving a new conversation, or binding a contact bot also queues a refresh.

WhatsApp is the source for a mirrored group's title, description, picture and member/admin snapshot. Existing contact bots get the contact's saved name (or WhatsApp push name), profile picture and a bridge description. The gateway does not expose contact About text. A management inbox topic gets its name updated; the shared management group's identity is preserved. Missing/hidden pictures and unavailable descriptions preserve existing Telegram values. Identical pictures are not uploaded again.

In a member bot's private chat, `/sync` refreshes that bot's bound contact even if you have only talked to the person in a group. Complete the contact binding in your private chat with the master first. An unbound bot's `/start` and `/sync` explain that step; `/sync all` remains a master-bot command.

Hermes needs **Change Group Info** in each separate mirror group. New group-picker requests include it. Existing groups without that permission receive an **Allow info edits** button in the sync result: select that same mirror group and confirm the requested administrator rights. Hermes then checks the permission and queues another sync. Telegram can shorten group descriptions to its 255-character limit and contact-bot names to 64 characters.

Member snapshots refresh the bridge's knowledge and the profiles of bots already bound to those members. Sync does not create bots or change group membership. Missing registered member bots get Telegram add-to-group buttons for you to confirm. Metadata sync only reads WhatsApp metadata; it does not send messages, retrieve history or mark chats read. Scheduled runs with no changes or warnings stay quiet; manual runs receive a completion message.

Configure the schedule in `bridge.env`, then recreate the bridge service to apply it:

```dotenv
SYNC_CRON=0 * * * *
SYNC_TIMEZONE=Asia/Kolkata
```

Use `*/15 * * * *` for every 15 minutes, `0 */6 * * *` for every six hours, or `off` to disable scheduled checks. `/sync` and `/sync all` still work when the schedule is off. Commands go through the signed n8n management workflow; scheduled checks run independently of n8n's public connector. A local alternative queues the same durable work:

```sh
docker compose exec -T wa-telegram-bridge node scripts/queue-sync.ts all
```

Replace `all` with a known WhatsApp conversation ID to refresh just that group or contact. Metadata lookup/update failures are reported per target so other targets can finish.

## Media and API differences

Text, quotes, photos, videos, documents, audio, voice, stickers, contact cards, locations and poll creation have explicit mappings. Voice/image conversion uses FFmpeg where needed. Telegram animated/video stickers fall back to their original document when not a WhatsApp-compatible WebP sticker. Albums arrive as individual attachments with captions. Native Telegram reactions and explicit `/react` send reactions to WhatsApp. Incoming WhatsApp reaction additions/removals now update native Telegram reactions instead of posting replies; a short attributed notice is used only when Telegram rejects the reaction. Poll-vote changes still appear as attributed notices.

In groups, the reactor's registered member bot reacts when present; Hermes handles unbound/absent participants. Telegram allows [one reaction per bot per message](https://core.telegram.org/bots/api#setmessagereaction), so participants sharing Hermes are represented by their latest active reaction rather than separate counts. In private chats, only the bot owning that DM is used. Full WhatsApp reaction snapshots and LID aliases are respected, emoji presentation variants are normalized, and Telegram-owner reaction echoes do not create a duplicate bot reaction when the account identity can be resolved.

Incoming poll choices appear as buttons. Poll voting and vote events require OpenWA's whatsapp-web.js engine; your inspected running gateway uses that engine. Multiple-choice buttons toggle the owner's selected choices. Unsupported API operations fail visibly rather than pretending to succeed.

Accessible view-once/disappearing content is archived as requested; inaccessible content gets a placeholder. When one of **your own messages** is deleted on WhatsApp, its Telegram content is preserved and gets **Delete / Keep** choices. Bot-posted copies get inline buttons; messages you posted directly in Telegram get a short reply with the choices when Telegram will not let the bot attach buttons to the original. **Delete** removes that message's text/media parts in the selected chat/topic; **Keep** dismisses the choices and retains the original content. Choices expire after the configured interactive lifetime (10 minutes by default), keeping the copy. Other people's deleted messages continue to be removed automatically. Deletion limits/permissions can prevent removal; remaining content is kept and a temporary notice explains the failure. Mappings remain recorded so history import does not resurrect deleted messages. Ordinary Telegram message deletion emits no reliable Bot API update, so use `/delete` to request deletion on WhatsApp. Calls and other unsupported message types produce notices; live calls, statuses and channels are not bridged.

The local Telegram server supports large files, but `MAX_MEDIA_BYTES` defaults to **100 MiB** to bound OpenWA's base64 sends and service memory use. OpenWA can impose a lower request/media cap: match that cap before raising this setting. Incompatible/oversized media gets an explicit error or placeholder. Delivered workspace files expire after 24 hours; the Telegram archive and message mappings persist. History completeness depends on what the linked OpenWA engine can retrieve; an offline bridge cannot promise recovery of Telegram updates older than Telegram's retention window.

## Delivery review and maintenance

```sh
docker compose ps
docker compose logs -f --tail=50 wa-telegram-bridge
docker compose exec wa-telegram-bridge node scripts/review.ts list
```

Only known rate refusals and safe reads are automatically retried. A timeout during a send pauses that conversation as **uncertain**. Inspect both chats before acting:

```sh
# Already delivered, or deliberately discard this job:
docker compose exec wa-telegram-bridge node scripts/review.ts skip JOB_ID
# Confirmed absent from the destination; retry with the same idempotency key:
docker compose exec wa-telegram-bridge node scripts/review.ts retry JOB_ID --confirmed-not-delivered
```

Skipping an uncertain send accepts that its reply mapping may be unavailable; future replies to that Telegram message may be unquoted. External API effects are journaled individually; completed effects are not replayed. Review jobs promptly: OpenWA's send idempotency records have a bounded retention window.

After env edits, run `npm run configure`, then `docker compose up -d --no-deps wa-telegram-bridge telegram-api` from the repository root. Restarting alone does not reload env files. Preserve the `bridge-data`, `telegram-data`, and `bridge-media` volumes. To stop this bridge, stop only its two services; do not remove volumes or alter the existing n8n/OpenWA data.

## Validation

```sh
npm test
npm run check:repository
```

Tests use fixtures and local HTTP servers. They cover routing, bot fallback, encrypted tokens, owner checks, signature verification, quoted replies, history, media dispatch, rate limits and crash recovery. They do not send to real contacts or prove live WhatsApp/Telegram interoperability. Local HTTP tests need permission to bind loopback.
