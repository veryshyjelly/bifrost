# Bifrost

A standalone personal WhatsApp–Telegram bridge. OpenWA supplies WhatsApp; Bifrost runs its own durable queue and represents contacts with Telegram bots. One contact bot represents one person across private chats and mirrored groups. Only the configured Telegram owner can operate it.

Bifrost and the local Telegram Bot API communicate over Docker networking. No n8n instance, workflow, public domain or tunnel is required. OpenWA is installed and paired separately.

## Moving to another PC

Follow [Moving Bifrost to another PC](docs/MOVING.md) to export this installation, import its existing credentials and state into a fresh clone, configure a new OpenWA API key/session without changing the saved account identity, and switch machines safely.

## Start a fresh identity

```sh
bun install --frozen-lockfile
bun run configure
cp .env.example .env
```

Fill the empty fields in private `bridge.env`, then run `bun run configure` again to generate the matching `telegram-api.env`. Use a dedicated Telegram master with **Bot Management Mode**, start it privately and add it as administrator to a **private forum supergroup** with Manage Topics and Delete Messages. Configure the ready OpenWA session and allow the `bifrost` callback hostname.

```sh
docker compose build bifrost telegram-api
docker compose up -d bifrost telegram-api
docker compose exec -T bifrost bun scripts/setup.ts
```

Use the installation and recovery guides at the selected revision. Preserve existing identity, credentials and state when restoring an account.

## Daily use

Reply in an inbox topic to send a WhatsApp message. The topic header includes buttons for **Create bot**, **Keep here**, **Mute**, **Ignore**, **Mark read**, and **Recent history**. In group topics, reply to a person's message with `/create` to request that person's bot.

Follow the creation link, then select the contact in your private chat with the master to confirm the binding. Start the new contact bot for private replies. Add it to mirrored groups where you want that sender identity. If a bot is absent, blocked, not yet started, or cannot be created because of your owned-bot limit, the master continues relaying with the person's name.

A bound member bot can send private messages even when the contact has only appeared in a group: its first ordinary DM creates the missing direct conversation route. Replies retain WhatsApp quotes. Phone-originated messages in an active dedicated conversation are archived through that same member bot, labeled as yours, so later replies can quote them in the DM.

History imports are manual. Run `/history 50` in a member bot's DM, a separate mirror group, or a linked management topic, then choose **Skip existing**, **Import all**, or **Cancel**. The management **Recent history** button offers the same choice. The choice prompt is removed after you select an option; the brief confirmation expires normally. No fetch starts and live messages continue while you decide. **Skip existing** skips copies recorded as already delivered in that destination. **Import all** creates fresh copies even when old delivery records remain, which is useful after clearing history directly in Telegram; it can create duplicates if those messages are still present. Telegram does not reliably notify the bridge of manual clearing. Choices expire after the interactive notice lifetime and can be used only once; run `/history` again for a new import.

After your choice, the complete available batch is fetched before posting, oldest first. In **Import all**, replies target new copies within that import; quotes outside its batch get a short preview instead of pointing at potentially cleared messages. Later live replies and reactions prefer the newest recorded copy. Other destinations and existing mappings remain intact, and messages revoked on WhatsApp remain excluded in either mode. Copies previously sent only to management can be imported into a new mirror. `/history` uses `HISTORY_LIMIT` (20 by default); explicit counts range from 1 to 2000. Fetching history sends no read receipt. Availability depends on OpenWA and WhatsApp; older attachments may be unavailable, with retained Telegram files used when available. A partial import resumes from saved receipts without duplicating completed parts.

History uses a retained date heading at the first delivered message and each day change, followed by compact messages: **Name [time]: body** through Bifrost, **[time]: body** through a member bot, or **You [time]: body** for your own WhatsApp messages. The word “History” and repeated dates are omitted from message bodies. Day boundaries and fallback labels use `SYNC_TIMEZONE` (Asia/Kolkata by default); Telegram localizes native dates and clocks for the viewer. Skipped messages do not create empty date sections. Date headings remain through `/clear log` and are removed along with chat history by `/clear all` in management. Existing imports and their saved presentation are preserved; new imports use the compact style.

History metadata stays separate from attachment retrieval, including deep imports. Attachments reuse retained Telegram files first, then OpenWA's stored/archive endpoint. A documented cache miss triggers one bounded WhatsApp download through OpenWA's `GET /messages/:chatId/:messageId/media/download?maxBytes=...` API (whatsapp-web.js). This requires an OpenWA version with that endpoint. A missing route/session is a configuration error, not proof that WhatsApp media expired. Temporary failures and rate limits retry; source unavailability or size limits leave the caption and a notice. Downloads preserve names/MIME types, and stickers use the native conversion path. They neither mark chats read nor view statuses. OpenWA's archive settings are unchanged; on-demand bytes are not automatically archived. Keep `BIFROST_ACCOUNT_ID` and the existing database when rotating OpenWA keys/sessions so receipts and partial-import checkpoints retain their identity. Updating does not replay completed imports.

Linking a separate mirror group or starting a bound member bot removes that conversation's old management topic and its messages. History is not copied automatically; invoke `/history <n>` in the new destination when desired. Bifrost needs Delete Messages permission in the management forum. Topic cleanup runs independently so a permission refusal does not prevent conversation delivery. Existing links are not swept on startup; retry linking or `/start` in the member bot to request cleanup.

Member-bot messages use Telegram's sender identity without repeating that person's name in the text, captions or edits. Bifrost uses **Name: message** on one line, with every name linked: to that person's WhatsApp chat when their phone number is known, or the general WhatsApp launch page otherwise. History uses the bracketed native time and daily date headings described above. Captions and edits use the same presentation. Live phone-originated messages keep their “sent from WhatsApp” marker; imported owner messages use **You [time]**. See [formatting capabilities and examples](docs/TELEGRAM-FORMATTING.md).

WhatsApp tags appear as readable **@Name** mentions. A bound member bot present in the destination group gets a native Telegram mention; an absent bot gets a link to its Telegram profile. People without bots get a WhatsApp-linked name, with the same generic WhatsApp link when their phone number is unknown. Only identities explicitly tagged by WhatsApp are converted; ordinary `@number` text stays literal. Text, captions, edits and manual history use the same conversion.

To tag someone from Telegram, select their member bot from Telegram's mention menu or type its `@bot_username`. Bifrost converts both username tags and Telegram's name-based mention entities into native WhatsApp mentions. Only Bifrost relays ordinary group messages, so tagged member bots do not produce extra sends.

For someone without a bot, send `/mention Abhishek` in the WhatsApp group's management topic or separate mirror group. Search by name, known number or bot username; `/mention` lists members with pagination. Tap **Copy tag** beside a result, then paste it anywhere in your message. Bound members copy their bot username; others copy a token such as `@{Abhishek|a1b2c3d4}`. Multiple tags can be mixed with ordinary text and used in captions or edits. The bridge replaces these tokens before sending to WhatsApp. The short ID distinguishes identical names and survives restarts or renames; changing the displayed name does not change whom the token tags. Copying never sends a message or marks WhatsApp read.

Mention searches use the synchronized group roster, refreshing it after five minutes. A failed refresh can show a clearly labeled cached picker, but sends requiring an expired roster are refused until it can refresh. Unknown, wrong-group or other-account tokens and members missing from the current roster produce a temporary correction notice without sending the message. Ordinary usernames unrelated to registered member bots stay literal. Mention conversion excludes code, email and URL entities. Retrying a previously prepared send retains its original resolved tags and delivery key.

Editing your Telegram text or media caption updates the existing WhatsApp message. If WhatsApp refuses the edit, a temporary reply explains that it was not applied and later messages continue normally. Clearing a caption is currently unsupported. Rate limits retain retry behavior, and uncertain delivery outcomes still require review.

WhatsApp Status posts share one **WhatsApp Status** topic in the management forum. Bifrost creates it on startup and imports currently available posts; new `status.received` events arrive automatically. `/statuses` in Bifrost opens the topic and refreshes available posts without duplicating the archive. Text, images, videos and voice statuses carry the poster's linked name and a native Telegram posting time. Missing/expired media gets a notice while its available caption is retained. Reading or refreshing this feed sends no WhatsApp view/read receipt.

Reply to a Status post with `/mute` or `/mute_status` to mute that person's Status posts in the bridge. Chats continue normally and existing archived posts remain. `/muted_statuses` shows a paginated list with **Unmute** buttons; replying to a Status post with `/unmute_status` also unmutes its author (`/keep` and `/unmute` work in this topic too). Preferences survive restarts and resolve known phone/LID aliases. Live events and refresh imports both honor mutes; posts skipped while muted stay skipped after unmuting. This changes only the Telegram feed, without sending any WhatsApp operation or read receipt.

The Status topic is an archive: source expiry does not remove its Telegram copies. It does not publish WhatsApp statuses or send replies/reactions; open the person's chat to respond. `/clear log` removes temporary notices while retaining archived posts; `/clear all` clears recorded posts in this topic. `/status` still shows bridge health.

To mirror a WhatsApp group, tap **Create / link Telegram group** in its management topic. In your private chat with Bifrost, tap **Create / select Telegram group**, create a private group (or select one you own), and confirm Bifrost's administrator permissions. The selected group is linked automatically. This uses Telegram's native picker and requires no separate account login. If your client has no create option in that picker, create the private group first and select it there.

For an older topic, send `/group` inside the topic to get the button. The manual alternative is to create a private Telegram group, add the master as an administrator, then send:

```text
/link <WhatsApp group ID from its inbox topic>
```

Your ordinary messages in that linked group go to the WhatsApp group. Bots cannot create Telegram groups or silently add other bots; use the supplied add-to-group links. A contact bot may be reused in multiple groups. Only the master consumes ordinary group messages, preventing one send per participant bot.

To return a mirror to management, send `/unmirror` to Bifrost inside that mirror group. Bifrost creates a new management topic and replies with an **Open management topic** button. Future messages use the topic; the former mirror keeps its existing messages and stops forwarding your messages and reactions to WhatsApp. History is not moved automatically: use `/history 50` in the new topic if needed. Mute/ignore settings are preserved; use `/keep` in the topic to resume them. Finish an active history import before using `/unmirror`. Bifrost needs Manage Topics permission in the management forum. You can use `/group` in the topic to create or link a mirror again.

Commands inside conversations:

| Command | Behavior |
| --- | --- |
| `/read` | Mark the WhatsApp conversation read |
| `/history [n]` | Choose whether to skip existing messages or import all; oldest first with replies, default 20, maximum 2000 |
| `/mention [search]` | Find a WhatsApp group member and copy a tag to paste into your message |
| `/create` | Request a contact bot; in groups, reply to that person's message |
| `/group` | Show the native create/select-and-link flow for a WhatsApp group |
| `/unmirror` | Move the current mirror group back to a new management topic |
| `/sync` | Refresh this conversation's WhatsApp metadata and existing member bots |
| `/sync all` | Refresh every active group and registered contact bot; use privately with Bifrost or in the management group |
| `/keep` | Resume or retain this conversation in the current topic/group |
| `/mute`, `/ignore` | Stop relaying new messages until `/keep` |
| `/delete` | Reply to a mapped message to request deletion on WhatsApp, subject to WhatsApp permissions |
| `/react 👍` | Reply to a mapped message to set a WhatsApp reaction; `/react` clears it |
| `/status` | Show counts, queue state, metadata schedule and last sync result |
| `/statuses` | Open the shared WhatsApp Status archive and refresh available posts through Bifrost |
| `/mute_status`, `/unmute_status` | Reply to a Status post to mute or unmute only its author's Status posts |
| `/muted_statuses` | List muted people and tap Unmute; accepts an optional page number |
| `/clear log`, `/clear_log`, `/clear` | Remove recorded bot notices and command messages in the current Telegram chat/topic |
| `/clear all`, `/clear_all` | Remove recorded Telegram messages in the current management topic, including General; unavailable in separate mirror groups |

A successful outgoing reply schedules a read receipt. Merely receiving a Telegram relay does not mark WhatsApp read. Phone-originated sends are labeled and do not echo back.

Commands addressed to another bot are ignored by Bifrost. A member bot can handle commands explicitly addressed to its own username in a group; unqualified group commands belong to Bifrost. Telegram join/leave, title and topic service events are not relayed. Editing a command does not execute it again or send it to WhatsApp.

Commands and bot notices are temporary: they are deleted after **60 seconds**, with **10 minutes** for replies containing buttons or a group picker. Set `COMMAND_TTL_SECONDS` and `CONTROL_TTL_SECONDS` in `bridge.env` to change those lifetimes. Expiration survives restarts. WhatsApp conversation messages and attachments remain in the Telegram archive.

`/clear log` clears notices in its current chat/topic without changing WhatsApp messages, bot bindings or delivery mappings. `/clear all` works in every topic of the management group, including General and WhatsApp Status, and removes only recorded messages in the topic where it is invoked. Other topics are preserved. Separate mirror groups and bot DMs support notice cleanup only. The bridge can remove only messages it has recorded, and Telegram normally permits deletion only within 48 hours. The completion notice reports older/protected messages or permission failures. Bifrost needs **Delete Messages** in groups to remove the owner's commands and other bots' notices. Existing recorded history is indexed for manual clearing; it is not automatically expired on upgrade.

The master and all registered member bots install their command menus on each configured startup. New bots also receive menus when their webhook is installed. Telegram's `/` autocomplete lists the commands; the extra `/clear_all` menu item is scoped to the management group and clears only its current topic when executed.

## WhatsApp metadata sync

The bridge checks WhatsApp metadata **hourly, at the start of each hour in Asia/Kolkata**. Its five-field cron scheduler runs inside the bridge service and persists each scheduled run in SQLite. Messaging jobs have priority; overlapping requests join an existing run. The service must be running at the scheduled time. Linking a group, receiving a new conversation, or binding a contact bot also queues a refresh.

WhatsApp is the source for a mirrored group's title, description, picture and member/admin snapshot. Existing contact bots get the contact's saved name (or WhatsApp push name), profile picture and a bridge description. The gateway does not expose contact About text. A management inbox topic gets its name updated; the shared management group's identity is preserved. Missing/hidden pictures and unavailable descriptions preserve existing Telegram values. Identical pictures are not uploaded again.

In a member bot's private chat, `/sync` refreshes that bot's bound contact even if you have only talked to the person in a group. Complete the contact binding in your private chat with the master first. An unbound bot's `/start` and `/sync` explain that step; `/sync all` remains a master-bot command.

Bifrost needs **Change Group Info** in each separate mirror group. New group-picker requests include it. Existing groups without that permission receive an **Allow info edits** button in the sync result: select that same mirror group and confirm the requested administrator rights. Bifrost then checks the permission and queues another sync. Telegram can shorten group descriptions to its 255-character limit and contact-bot names to 64 characters.

Member snapshots refresh the bridge's knowledge and the profiles of bots already bound to those members. Sync does not create bots or change group membership. Missing registered member bots get Telegram add-to-group buttons for you to confirm. Metadata sync only reads WhatsApp metadata; it does not send messages, retrieve history or mark chats read. Scheduled runs with no changes or warnings stay quiet; manual runs receive a completion message.

Configure the schedule in `bridge.env`, then recreate the bridge service to apply it:

```dotenv
SYNC_CRON=0 * * * *
SYNC_TIMEZONE=Asia/Kolkata
```

Use `*/15 * * * *` for every 15 minutes, `0 */6 * * *` for every six hours, or `off` to disable scheduled checks. `/sync` and `/sync all` still work when the schedule is off. Commands and scheduled checks use Bifrost’s local durable queue. A local alternative queues the same durable work:

```sh
docker compose exec -T bifrost bun scripts/queue-sync.ts all
```

Replace `all` with a known WhatsApp conversation ID to refresh just that group or contact. Metadata lookup/update failures are reported per target so other targets can finish.

## Media and API differences

Text, quotes, photos, videos, documents, audio, voice, stickers, contact cards, locations and poll creation have explicit mappings. Voice/image conversion uses FFmpeg where needed. WhatsApp animated WebP stickers become native Telegram VP9 WebM stickers; Telegram TGS and WebM stickers become animated WebP for WhatsApp. Static stickers remain static. Transparency and frame timing are preserved; animations longer than Telegram’s three-second limit are sped up to retain the complete sequence. Unsupported, oversized or failed conversions retain the original-document fallback. See [sticker conversion and local checks](docs/STICKERS.md). Albums arrive as individual attachments with captions. Native Telegram reactions and explicit `/react` send reactions to WhatsApp. Incoming WhatsApp reaction additions/removals now update native Telegram reactions instead of posting replies; a short attributed notice is used only when Telegram rejects the reaction. Poll-vote changes still appear as attributed notices.

In groups, the reactor's registered member bot reacts when present; Bifrost handles unbound/absent participants. Telegram allows [one reaction per bot per message](https://core.telegram.org/bots/api#setmessagereaction), so participants sharing Bifrost are represented by their latest active reaction rather than separate counts. In private chats, only the bot owning that DM is used. Full WhatsApp reaction snapshots and LID aliases are respected, emoji presentation variants are normalized, and Telegram-owner reaction echoes do not create a duplicate bot reaction when the account identity can be resolved.

Enable **Bot-to-Bot Communication** for Bifrost in BotFather so it can access messages posted by member bots; Bifrost's group administrator role is already required. Enable it for member bots too if each should react under its own identity to other bots' messages. If a member bot cannot access another bot's message, Bifrost attempts the native reaction instead. Refused reactions can be retried on later events after one minute so updated permissions can recover. Bots' ordinary messages remain excluded from WhatsApp relay, preventing loops. See [Telegram's bot-to-bot rules](https://core.telegram.org/bots/features#bot-to-bot-communication).

Telegram custom emoji reactions are resolved to their associated standard emoji before forwarding to WhatsApp, including reactions in member-bot DMs. When Telegram supplies several reactions, a newly added one is preferred; WhatsApp supports one reaction from your account per message. Unknown custom or paid reactions produce a temporary explanation and do not clear the WhatsApp reaction. Successful lookups and prepared outgoing choices are cached so retries retain the same emoji.

A small set of WhatsApp emojis without standard Telegram reactions use clear equivalents: 😂 → 🤣; 😀/😃/😄/😆 → 😁; 😠 → 😡; 😔/😞/😥 → 😢; 😙/😚 → 😘; 🥳 → 🎉. Other emojis are attempted unchanged. If Telegram rejects an exact or equivalent reaction, the temporary notice shows the original WhatsApp emoji. Source identities, removals and outgoing Telegram-to-WhatsApp reactions keep their original values; existing fallback notices are not rewritten on upgrade.

Incoming poll choices appear as buttons. Poll voting and vote events require OpenWA's whatsapp-web.js engine; your inspected running gateway uses that engine. Multiple-choice buttons toggle the owner's selected choices. Unsupported API operations fail visibly rather than pretending to succeed.

Accessible view-once/disappearing content is archived as requested; inaccessible content gets a placeholder. When one of **your own messages** is deleted on WhatsApp, its Telegram content is preserved and gets **Delete / Keep** choices. Bot-posted copies get inline buttons; messages you posted directly in Telegram get a short reply with the choices when Telegram will not let the bot attach buttons to the original. **Delete** removes that message's text/media parts in the selected chat/topic; **Keep** dismisses the choices and retains the original content. Choices expire after the configured interactive lifetime (10 minutes by default), keeping the copy. Other people's deleted messages continue to be removed automatically. Deletion limits/permissions can prevent removal; remaining content is kept and a temporary notice explains the failure. Mappings remain recorded so history import does not resurrect deleted messages. Ordinary Telegram message deletion emits no reliable Bot API update, so use `/delete` to request deletion on WhatsApp. Calls and other unsupported message types produce notices; live calls and channels are not bridged. Incoming WhatsApp Status posts use the separate archive topic described above.

The local Telegram server supports large files, but `MAX_MEDIA_BYTES` defaults to **100 MiB** to bound OpenWA's base64 sends and service memory use. OpenWA can impose a lower request/media cap: match that cap before raising this setting. Incompatible/oversized media gets an explicit error or placeholder. Delivered workspace files expire after 24 hours; the Telegram archive and message mappings persist. History completeness depends on what the linked OpenWA engine can retrieve; an offline bridge cannot promise recovery of Telegram updates older than Telegram's retention window.

## Delivery review and maintenance

```sh
docker compose ps
docker compose logs -f --tail=50 bifrost
docker compose exec bifrost bun scripts/review.ts list
```

Only known rate refusals and safe reads are automatically retried. A timeout during a send pauses that conversation as **uncertain**. Inspect both chats before acting:

```sh
# Already delivered, or deliberately discard this job:
docker compose exec bifrost bun scripts/review.ts skip JOB_ID
# Confirmed absent from the destination; retry with the same idempotency key:
docker compose exec bifrost bun scripts/review.ts retry JOB_ID --confirmed-not-delivered
```

Skipping an uncertain send accepts that its reply mapping may be unavailable; future replies to that Telegram message may be unquoted. External API effects are journaled individually; completed effects are not replayed. Review jobs promptly: OpenWA's send idempotency records have a bounded retention window.

When Telegram definitively rejects a relay because its management topic was deleted, the bridge can recover an entirely undelivered message into the ready contact DM or a recreated inbox topic. It preserves recorded/uncertain delivery targets for review. Missing targets for old reactions and edits do not block new conversation messages, and cleared or cross-topic copies are not used as quote targets.

After env edits, run `bun run configure`, then `docker compose up -d --no-deps bifrost telegram-api` from the repository root. Restarting alone does not reload env files. Preserve the `bridge-data`, `telegram-data`, and `bridge-media` volumes. To stop this bridge, stop only its two services; do not remove volumes or alter the existing n8n/OpenWA data.

## Validation

```sh
bun run test
bun run check:repository
```

Tests use fixtures and local HTTP servers. They cover routing, bot fallback, encrypted tokens, owner checks, signature verification, quoted replies, history, media dispatch, rate limits and crash recovery. They do not send to real contacts or prove live WhatsApp/Telegram interoperability. Local HTTP tests need permission to bind loopback.

The bridge runtime, scripts, tests and container use Bun. Its Node-compatible built-in module imports run within Bun; they do not start a Node process. OpenWA retains its independent runtime requirements. See [Bun migration and rollback](docs/BUN-MIGRATION.md).
