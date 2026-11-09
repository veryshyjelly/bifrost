# Behavioral specification

Labels V/U/D/N are defined in [01](01-current-architecture.md). V below means local implementation evidence; each provider-dependent result is U until checked against the deployed pinned services. Preserve literal messages, buttons, method ordering, entity offsets and effect keys through golden fixtures, not just the summaries here.

## Ingress, authorization and delivery ledger — V

Trigger: incoming HTTP request (`src/server.ts:8`). GET /healthz is public and reports process/configuration health, not dependency readiness. GET /v1/status and POST /v1/review require Bearer admin. POST /webhooks/telegram/{bot} requires x-telegram-bot-api-secret-token derived from bot ID/admin HMAC. OpenWA signature is sha256 HMAC over the exact raw body, checked after a 2 MiB body cap. Invalid JSON/non-object input is 400, bad authentication 401, unknown paths 404, oversized input 413. Management actions contain their own JSON-body HMAC and must be within seven days (`bridge.ts:102`). They do not use HTTP Bearer authentication.

OpenWA admission requires configured session, supported event, idempotencyKey and data; dedup key is ow:{key}, stream wa:{chat or session}; account identity is stamped at ingress (`bridge.ts:78`). Events: message.received/sent/edited/revoked/reaction, poll.vote, status.received, session.status/disconnected, message.failed (`src/openwa.ts:4`). Webhook setup creates missing callback with five retries or merges subscriptions without replacing other secrets/settings.

Telegram admission requires registered bot, numeric safe update_id and non-bot owner actor. Ordinary member-bot group messages are ignored; callbacks, membership and explicitly addressed commands are admitted. Group messages are processed via manager to avoid overlap; member private IDs include bot scope. Observation for cleanup precedes owner filtering in recognized chats, so indexing does not imply authorization (`bridge.ts:83`).

Jobs become running and increment attempts when selected. Successful jobs become done. Retryable errors retry below eight attempts with maximum of provider delay and exponential delay capped at 300 seconds; ambiguous mutation outcomes become uncertain; definitive errors become failed (`bridge.ts:107`). Failed and uncertain jobs block later jobs in the same stream. Failures queue separate safe management notices except recursive/control cases.

Effects persist sending before remote calls, done with receipt after success, pending after definite API refusal, uncertain after ambiguous or generic failure (`bridge.ts:64`). Restart explicitly changes sending→uncertain and running→pending (`store.ts:33`). Replaying a done effect returns its saved result. Uncertain effects never automatically resend. Review retry requires confirmNoDelivery for uncertain jobs, resets only uncertain effects belonging to that job (plus stable outgoing Telegram-message send key), preserves done effects; skip finishes job (`server.ts:38`). This is conservative delivery, not an exactly-once guarantee.

## First contact, Managed Bot provisioning and binding — V, provider execution U

Trigger: eligible new WhatsApp message. Resolve canonical peer/author; learn phone/LID alias when present, reject conflicting aliases; retain authoritative synced contact name. Save message provenance. Unknown private/group conversations create manager forum topic, route and new_conversation management job. First arrival is not unsolicited registration in manager DM: conversation controls appear in its topic (`bridge.ts:204`, `294`, `825`). Channels and ordinary broadcast/status-like messages are excluded; Status has its own path.

```mermaid
sequenceDiagram
  participant W as OpenWA
  participant B as Bifrost
  participant S as SQLite
  participant T as Telegram
  participant O as Owner
  W->>B: signed message.received
  B->>S: enqueue and claim
  B->>T: createForumTopic (effect receipt)
  B->>S: route + new_conversation
  B->>T: relay + conversation controls
  O->>B: /create or Create bot callback
  B->>S: provision pending
  B->>T: getMe and official newbot link
  O->>T: complete Managed Bot creation
  T->>B: managed_bot / managed_bot_created
  B->>T: getManagedBotToken
  B->>S: encrypt credential, ready=false
  B->>T: hosted logOut once; local webhook and menus
  B->>O: DM contact binding buttons
  O->>B: bind callback
  B->>S: contact.bot + provision bound + dedicated route
  B->>O: Start / Add to group links
  O->>B: start bound bot privately
  B->>S: ready=true; queue inbox retirement
```

`/create` in group must reply to participant; it uses mapped WhatsApp sender, not the group identity. Existing bot produces Add guidance. New request stores provision pending and sends `https://t.me/newbot/{manager username}/{deterministic contact username}?name=...`; no automated token provisioning or BotFather/MTProto exists (`bridge.ts:771`, `858`). Creation refusal leaves manager routing available; no bot-capacity API is queried.

Registration checks owner, gets token with getManagedBotToken user_id=bot ID, AES-GCM seals token, preserves readiness for existing record, installs webhook, and for a new bot offers first 20 pending contacts in manager DM (`bridge.ts:781`). Association is explicitly chosen, not inferred from creation order or generated username. Existing bot registration refreshes token/webhook without re-offering binding. Binding requires pending request/contact/bot and rejects a bot already assigned to another person. Store changes precede confirmation; metadata sync queues separately. Private /start or ordinary bound DM marks ready; an unbound DM explains binding. A ready dedicated bot sends to owner DM; absent readiness falls back to inbox. Retirement deletes old topic separately after shared-topic/General guards (`bridge.ts:220`).

## WhatsApp → Telegram routing and presentation — V

Trigger: eligible received/sent message (`bridge.ts:294`). Suppress revoked IDs, completed live messages and outbound mapped echoes. Apply latest saved edit before first delivery. Route modes muted/ignored stop relay. Status always uses manager; own group messages use manager; participant bots are preferred only if live getChatMember confirms presence. Dedicated private bot requires ready. Freeze destination, bot, formatting version and presentation before sends; preserve on retries (`bridge.ts:285`, `329`).

Manager presentation attributes sender; participant bot omits redundant header. Own sends identify You and WhatsApp origin. Formatting uses Telegram entities, UTF-16 offsets, sender links, historical time/day labels and safe splitting (`src/format.ts:1`, `test/format.test.ts`). Native replies require surviving mapped copy in same destination/thread and same bot for private DM. History may show italic truncated quote preview if parent is outside window. Every emitted part maps to same WhatsApp source; completion is separate from first-part receipt.

Telegram 403 from preferred bot retries via manager with distinct fallback snapshot. Missing management topic is repaired only on recognized thread_missing and before any delivered/uncertain part; recreation or ready DM selection omits stale quotes. History replay does not repair/redirect target this way (`bridge.ts:146`).

## Telegram → WhatsApp — V

Trigger: owner ordinary message in linked topic/group or bound contact DM (`bridge.ts:516`). Resolve route by exact topic when configured; whole-group routes accept Telegram incidental reply-thread IDs. Exclude edited/malformed commands, service events, Status archive messages, muted/ignored routes and already mapped sends. Quoted mapping must belong to same WhatsApp chat. Resolve mentions before send; rejected mentions show actionable notice without sending.

```mermaid
sequenceDiagram
  participant T as Telegram webhook
  participant B as Bifrost worker
  participant S as SQLite
  participant W as OpenWA
  T->>S: bot/update dedup, scoped stream
  S->>B: next job
  B->>S: route, quote, frozen mentions/media
  B->>S: effect sending (stable send key)
  B->>W: messages/reply or media/text + hashed Idempotency-Key
  alt receipt contains messageId
    W-->>B: receipt
    B->>S: effect done + outbound mapping
    B->>S: separate mark_read job
  else ambiguous outcome
    B->>S: uncertain effect/job; block stream
  end
```

Stable key `send:{bot: for private}{chat}:{message_id}` survives overlapping update IDs; SHA256 of that key is outgoing OpenWA idempotency header. Routes select reply/send-text; media uses send-image/video/audio/sticker/document; voice sets ptt; location/contact/poll have dedicated endpoints. Missing returned messageId is uncertain. Mark-read failure cannot replay send (`bridge.ts:594–657`). Incoming WhatsApp echoes with outbound mapping do not loop.

Edited Telegram copies only edit mapped outbound messages, calling messages/edit with body and explicit mentions array. Caption clearing is refused locally; definite OpenWA 400/403/404/413/422 refusal records refusal and queues independent notice, so later sends continue. Ambiguous/rate-limited edits retain normal conservative recovery (`bridge.ts:602`). Telegram poll_answer is admitted but no forwarding branch exists (N); WhatsApp polls are rendered as text plus vote buttons, not native Telegram polls.

## Media and structured content — V; codecs/provider acceptance U

`src/media.ts:13`: private random filenames, bounded extensions/size, no trust in advertised arbitrary media URLs. WhatsApp media comes from authenticated pinned session endpoints or embedded base64. Telegram getFile uses largest photo; absolute file paths must realpath under /telegram-data; otherwise download only from configured API. Streaming enforces cap even without content-length. Files older than one day are cleaned hourly (`media.ts:79`).

Inbound types image/video/audio/voice/sticker/document use native Telegram methods. Missing/oversize media (404/413) creates caption plus unavailable placeholder and completes delivery. Sticker conversion 415 falls back to original document; definitive native upload 400/415 also falls back, excluding missing reply/thread errors. Labels split from stickers and long captions; earlier successful parts are retained on retry (`bridge.ts:385–445`). Voice/image conversion uses FFmpeg and may fall back to document. Locations send label plus native location; vCards parse FN/TEL and sendContact or text; poll shows up to 12 choice buttons, toggles multi-selection and sends messages/vote-poll. Unsupported/call types show readable annotation (`bridge.ts:446`).

Outgoing Telegram photo/video/animation/video_note/audio/voice/document/sticker are downloaded and base64 submitted. Sticker conversion choice, metadata and byte hash freeze before send; missing cached bytes must regenerate identically or fail for review (`bridge.ts:625`). Global sticker serialization spans Media instances; detached Bun worker has 120-second timeout and kills descendants, cleans private intermediates (`src/stickers.ts:13`). Worker validates dimensions/durations, rejects Lottie expressions/external assets, dynamically imports Puppeteer and resolves lottie_light; Python/Pillow and FFmpeg preserve alpha/timing with bounded compression attempts (`sticker-worker.ts:1`, `scripts/sticker-webp.py:1`).

## Changes, reactions and revocations — V

WhatsApp edits store latest-edit before processing copies, suppress outbound edit echoes, update surviving mapped text/captions using saved presentation; missing copies/topics stay absent; other 400 refusal produces attributed edit notice. Split edits use first formatted part for each existing copy, rather than replanning a new multi-message relay (`bridge.ts:469`). Revocation stores tombstone even if original event arrives later; history must not resurrect it.

`src/revocations.ts:23`: other people's revoked messages delete Telegram copies through independent durable controls. Own revoked messages offer Delete/Keep, first inline markup then reply prompt fallback. Controls validate owner/session/expiry, maintain cursor, preserve mappings, handle blocked/deleted messages and retry idempotent deletion independently. Keep and expiration preserve the original archive and remove only controls/prompt; blocked deletion yields a partial result and notice (`revocations.ts:110`).

`src/reactions.ts:19`: per-source actor reaction state, full snapshot reconciliation and latest actor per available bot; prefer attachment/latest destination copy, skip deleted copies. Standard emoji equivalents are explicitly mapped for Telegram, original emoji retained in source/fallback. Absent/refused bot falls back to manager; unsupported native reaction becomes attributed text. Owner reaction echoes suppress loops. Telegram custom emoji uses getCustomEmojiStickers, validates one Unicode grapheme, caches resolution and freezes outgoing decision. Unresolvable emoji produces separate notice without clearing prior WhatsApp reaction. Definitive refusal caches expire; idempotent reaction/delete controls may retry ambiguous outcomes differently from sends.

## History, Status, metadata and cleanup — V

History (`src/history.ts:45`): /history 1–2000 offers Skip existing / Import all / Cancel; prompt alone does not pause live delivery. Owner/bot/account/target/thread/expiry/binding validated. Atomic selection creates one active run per conversation. Fetch complete batch, use deep=true above 100, freeze plan; filter wrong-chat/revoked/Status rows, dedup IDs, sort timestamp/ID with parents before children and cycle guard. Queue ordered items and finish; live events for this conversation wait, other chats continue. Destination-scoped completion skips already delivered copies; Import all uses fresh per-run copies and quote mappings. Day markers and compact history formatting freeze timezone/style; old runs preserve legacy style. Archived Telegram media can outlive OpenWA media. Failures mark run failed and release history hold; partial parts keep receipts. Count in final notice is available messages checked, not a count of newly sent messages (`history.ts:143`).

Status (`src/statuses.ts:1`): one account-scoped manager archive topic, startup merges subscriptions and fetches available status; normalize timestamps, reject malformed/expired posts before media, dedup refresh/webhook races. Text/image/video/voice supported; unknown type annotated. Archive remains after source expiry. It is read-only toward WhatsApp. Per-author mutes suppress before downloads and do not mute conversation; unmute resumes future posts without replaying skipped ones. Paged mute/unmute controls validate context and revision so stale buttons/retries cannot reverse newer choices.

Metadata (`src/sync.ts:23`): manual/new/bind/link/cron requests coalesce active scopes and attach waiters. All enumerates active group routes and bound bots. Group roster snapshots validate completeness before replacement; inbox sync changes topic name only; mirror sync verifies private group and Change Info before title/description/CDN-allowlisted photo updates. Missing bots yield Add links, never automatic membership synchronization. Contact sync refreshes name, topic, bot name/description/photo. Photos hash/cache; absent photos are skipped, not deleted. Target failures become warnings after limited retries; cron with no changes/warnings/roster changes is quiet. Finish reports persisted results.

Controls (`src/control.ts:27`): command/notice ledger is bot-scoped in private, shared in groups. Normal chat copies never auto-expire. Commands/notices expire at command TTL, interactive replies at control TTL. Menus install on setup and migrated-bot startup. /clear log removes recorded notices/commands in current chat/topic; /clear all only through manager in management group, including General, bounded by snapshot sequence cutoff. Skip protected/48-hour-old messages, fall back from batch to single on 400; 403 produces permission notice, transient/ambiguous deletion retries. Preserve bindings/mappings. Retirement and unmirror are separate from history: linking/starting queues old-topic deletion, never automatic import.

## Group linking and return — V, Telegram picker UI U

/group yields account/owner/route-scoped 24-hour manager DM deep link; /start mirror_* creates request_chat keyboard with random request ID and requested admin rights (`bridge.ts:880`). chat_shared must match request and unchanged source route. link_group verifies existing WhatsApp route, private Telegram group, manager administrator and unique destination, persists group mode/inbox provenance, marks request used, confirms, queues metadata and old inbox retirement (`bridge.ts:806`). /link provides manual equivalent. No automatic Telegram member creation/invitation or WhatsApp membership mutation exists (N).

/unmirror requires manager in current linked mirror and no active history. Create new management topic with effect receipt, atomically persist route plus inbox-return checkpoint, keep mute/ignore, rebuild controls and notify former group with topic link; archived messages remain. Definitive permission failure leaves mirror intact. Retried/stale requests cannot undo later links; uncertain topic creation requires review (`bridge.ts:248`).

## Command surface

/start, /inbox, /status, /create, /group, /link, /unmirror, /read, /history, /keep, /mute, /ignore, /delete, /react, /mention, /sync [all], /statuses, /mute_status, /unmute_status, /muted_statuses, /clear [log|all], /clear_log, /clear_all. Context restrictions and malformed usage replies are in `bridge.ts:659`, `control.ts:6`, `mentions.ts:132`, `statuses.ts:60`. Commands addressed to another bot are ignored; edits of commands do not become sends. General operator callbacks use persisted UUID action records (`bridge.ts:466`); do not silently add expiry/context rules from newer specialized controls to these legacy controls.

## Evidence index

The following observable test cases form a baseline inventory, not a claim of live provider validation. Opt-in codec and Docker cases were not run. Line numbers identify exact characterization evidence.

### bridge.test.ts

- `test/bridge.test.ts:45` — standalone management and legacy forwarding jobs converge on one durable action without HTTP forwarding.
- `test/bridge.test.ts:57` — a group-only contact bot creates its direct route without a management topic and sends ordinary and quoted replies.
- `test/bridge.test.ts:71` — unbound member DMs explain the missing binding instead of silently dropping the message.
- `test/bridge.test.ts:78` — phone-originated private messages stay in their started member-bot DM and can be quoted by incoming replies.
- `test/bridge.test.ts:89` — a deleted inbox topic is recreated after a definitive pre-delivery refusal and stale quotes are omitted.
- `test/bridge.test.ts:101` — an old frozen master target can recover into the ready contact DM only before any delivery.
- `test/bridge.test.ts:117` — a rejected media relay moves to the ready member DM without trying document fallback in the missing topic.
- `test/bridge.test.ts:134` — a blocked member can fall back even when its management topic was deleted.
- `test/bridge.test.ts:150` — edits of Telegram copies explicitly cleared from the archive do not recreate them.
- `test/bridge.test.ts:159` — unknown conversation gets one topic; replay is deduplicated and never marks read.
- `test/bridge.test.ts:169` — dedicated bot wins in a group only when present; master falls back with sender name.
- `test/bridge.test.ts:183` — member-bot media, stickers, locations, cards and polls omit redundant sender headers.
- `test/bridge.test.ts:200` — member-bot history keeps a compact time and body while phone-originated sends keep owner attribution.
- `test/bridge.test.ts:214` — member-bot edits do not restore a sender header; master edit fallback remains attributed.
- `test/bridge.test.ts:227` — private member-bot messages and edit fallbacks stay nameless in the correct bot DM.
- `test/bridge.test.ts:239` — Telegram reply maps to WhatsApp quote; send echoes and overlapping bot updates do not loop.
- `test/bridge.test.ts:256` — /create in a linked whole group accepts Telegram reply-thread IDs and selects the replied-to participant.
- `test/bridge.test.ts:280` — ordinary replies in a whole group share one queue stream and retain WhatsApp quotes.
- `test/bridge.test.ts:295` — configured inbox topics still require an exact topic match.
- `test/bridge.test.ts:309` — whole-group reply-thread handling still rejects a quoted message from another WhatsApp conversation.
- `test/bridge.test.ts:318` — uncertain remote send pauses a conversation, while known 429 retries.
- `test/bridge.test.ts:329` — sealed tokens authenticate and are absent from SQLite plaintext.
- `test/bridge.test.ts:336` — management signatures reject forgery and repeated actions are durable.
- `test/bridge.test.ts:343` — out-of-order WhatsApp revocation uses revokedId and prevents history resurrection.
- `test/bridge.test.ts:350` — phone/LID aliases share a contact bot across conversations.
- `test/bridge.test.ts:358` — each WhatsApp media class uses native Telegram methods; view-once content exposed by OpenWA is archived.
- `test/bridge.test.ts:368` — converted WhatsApp stickers use WebM filenames and preserve quote mappings across a partial retry.
- `test/bridge.test.ts:384` — sticker conversion failures and definitive Telegram rejections send the untouched original document.
- `test/bridge.test.ts:400` — outgoing TGS and WebM stickers use converted WebP with replies and the original delivery key.
- `test/bridge.test.ts:419` — ordinary documents bypass conversion and failed outgoing sticker conversions keep original bytes.
- `test/bridge.test.ts:435` — rate-limited sticker sends freeze the conversion choice and bytes even after a converter recovers.
- `test/bridge.test.ts:452` — missing media is visible, and location/contact/poll use structured representations.
- `test/bridge.test.ts:465` — dedicated private bot waits for Start, then routes private replies to the contact.
- `test/bridge.test.ts:478` — muted conversations can resume; creating a bot at capacity leaves master routing intact.
- `test/bridge.test.ts:491` — 429 is retried with the same WhatsApp idempotency key.
- `test/bridge.test.ts:503` — child-bot group callbacks remain actionable while its ordinary group updates are ignored.
- `test/bridge.test.ts:509` — private message ids are isolated between contact bots.
- `test/bridge.test.ts:523` — a partial sticker delivery retries the attachment without losing it or duplicating the label.
- `test/bridge.test.ts:539` — blocked dedicated bot falls back to its management topic on a definitive Telegram refusal.
- `test/bridge.test.ts:552` — completed external effects retain their original destination when routing changes during a retry.
- `test/bridge.test.ts:564` — history is bounded and enqueued with historical markers.
- `test/bridge.test.ts:573` — opening a second store does not reset a live process; explicit recovery preserves uncertain sends.

### configure.test.ts

- `test/configure.test.ts:8` — fresh configuration generates independent private secrets from the tracked template.
- `test/configure.test.ts:21` — reconfiguration preserves existing encryption keys and all operator configuration.

### control.test.ts

- `test/control.test.ts:43` — Bifrost ignores commands for another bot, while the addressed member bot handles its own group Start.
- `test/control.test.ts:56` — join, leave, title and forum service events produce no unsupported-message replies or WhatsApp operations.
- `test/control.test.ts:65` — edited commands and edits of former commands never become WhatsApp messages.
- `test/control.test.ts:74` — commands and notices expire durably, while WhatsApp text and poll relays remain ordinary chat messages.
- `test/control.test.ts:92` — interactive command replies get a longer lifetime.
- `test/control.test.ts:99` — observing a member-bot notice through Bifrost keeps its expiration and owner-only execution checks.
- `test/control.test.ts:109` — /clear log removes local notices and command messages, preserving chat messages, bindings and mappings.
- `test/control.test.ts:123` — /clear all in management General preserves other topics and old/protected messages.
- `test/control.test.ts:138` — both clear-all forms clear only the current management topic, preserving other chats, later messages and mappings.
- `test/control.test.ts:161` — clear all remains unavailable in mirror groups, private chats and through member bots.
- `test/control.test.ts:171` — cleanup isolates private message IDs for each member bot.
- `test/control.test.ts:181` — one undeletable message does not prevent removal of other notices.
- `test/control.test.ts:191` — ambiguous cleanup failures retry as idempotent deletes without uncertain delivery effects.
- `test/control.test.ts:202` — startup registers command menus for migrated master/member bots, including management-topic clear_all.
- `test/control.test.ts:220` — existing recorded chat/history is indexed for cleanup without expiring mirrored conversations.

### format.test.ts

- `test/format.test.ts:48` — master relay uses a bold WhatsApp-linked name and body on one line.
- `test/format.test.ts:54` — verified phone JIDs open their chat; unknown identities use the generic WhatsApp launch link.
- `test/format.test.ts:62` — untrusted HTML characters remain literal text and cannot inject markup or links.
- `test/format.test.ts:71` — compact history uses linked sender and bracketed native time while member bots omit their names.
- `test/format.test.ts:84` — invalid source timestamps remain clearly historical without inventing a date.
- `test/format.test.ts:90` — compact clocks handle invalid timestamps and dates group in the configured timezone.
- `test/format.test.ts:103` — saved history presentations retain legacy rendering when compact style was not selected.
- `test/format.test.ts:109` — formatted splitting preserves every UTF-16 character, clips links safely and keeps native time atomic.
- `test/format.test.ts:119` — caption entity offsets are safe and oversize raw captions must be handled as text.
- `test/format.test.ts:125` — relay transport carries native entities and disables the generated WhatsApp header preview.
- `test/format.test.ts:135` — sender links resolve known LID aliases rather than treating a LID as a phone number.
- `test/format.test.ts:141` — phone-originated private messages and edits never link You to the conversation partner.
- `test/format.test.ts:152` — You links only to the independently verified session phone.
- `test/format.test.ts:161` — long media captions keep all content in formatted text and a compact linked attachment header.
- `test/format.test.ts:173` — history metadata survives formatted message edits and captions do not contain raw HTML.
- `test/format.test.ts:183` — formatted packets remain immutable across a partial send retry.
- `test/format.test.ts:191` — presentation stays frozen while a media upload retries after a contact rename.
- `test/format.test.ts:201` — pre-upgrade partial deliveries retain their legacy packet boundaries.

### group-picker.test.ts

- `test/group-picker.test.ts:32` — native group creation/selection grants master rights and links only after the owner shares a group.
- `test/group-picker.test.ts:55` — expired and previous-session buttons produce guidance without opening a selector.
- `test/group-picker.test.ts:68` — unknown requests and other Telegram users cannot link a group.
- `test/group-picker.test.ts:78` — picker rejects public destinations and the management group.
- `test/group-picker.test.ts:88` — new group inbox controls include the button and /group works for existing topics.

### history.test.ts

- `test/history.test.ts:53` — queued history resumes under its original checkpoint after OpenWA credentials change.
- `test/history.test.ts:68` — an unexpired history choice remains usable after an OpenWA session rotation.
- `test/history.test.ts:80` — /history n fetches the complete batch first, posts oldest first, and recreates native reply chains.
- `test/history.test.ts:94` — history delivered to management can be imported into a mirror and repeated requests do not duplicate it.
- `test/history.test.ts:108` — history in a new member DM includes own messages and preserves bot-scoped reply targets.
- `test/history.test.ts:119` — history retries use the saved batch, preserve earlier parts and hold only live events for this conversation.
- `test/history.test.ts:138` — history orders parents before children even with equal or inconsistent source timestamps and previews outside-window quotes.
- `test/history.test.ts:146` — history reuses archived Telegram media even after its management topic has been removed.
- `test/history.test.ts:161` — manual history can restore a copy explicitly cleared from the current destination without reusing deleted receipts.
- `test/history.test.ts:169` — history sends retained WebM sticker media through the Telegram sticker preparation path.
- `test/history.test.ts:187` — history validates counts and uses the gateway deep-history option above 100.
- `test/history.test.ts:195` — history fetch failure releases live delivery and never deletes a management topic.
- `test/history.test.ts:203` — linking a mirror queues old-topic deletion but never imports history automatically.
- `test/history.test.ts:215` — starting a bound DM retires its inbox separately and cleanup refusal does not block messaging.
- `test/history.test.ts:228` — retiring a DM topic is durable and a blocked member recreates an inbox lazily.
- `test/history.test.ts:240` — General and management topics shared with another conversation cannot be deleted by retirement.
- `test/history.test.ts:249` — a retired direct route cannot capture General or another management topic.
- `test/history.test.ts:258` — history requests from a previous session cannot block the current session.
- `test/history.test.ts:265` — /history waits for a choice without fetching or holding live messages, and Cancel does not start an import.
- `test/history.test.ts:279` — Import all restores manually cleared messages despite old delivery records and links replies to the new copies.
- `test/history.test.ts:299` — Import all previews out-of-window quotes instead of replying to stale copies from a cleared chat.
- `test/history.test.ts:308` — Import all retries a frozen batch without duplicating parts and repeated clicks cannot start another run or switch mode.
- `test/history.test.ts:325` — Import all still excludes WhatsApp-revoked messages and preserves other destinations.
- `test/history.test.ts:340` — history choices validate owner, bot, session, destination, topic, binding and expiry.
- `test/history.test.ts:358` — member DM Import all remains bot scoped and legacy Recent history buttons now show the choice.
- `test/history.test.ts:375` — two pending choices cannot start concurrent imports or change an active import mode.
- `test/history.test.ts:383` — reactions prefer a fresh unavailable-media placeholder over a stale attachment from before clearing.
- `test/history.test.ts:397` — history posts one retained date marker per day with time-only messages and replies across midnight.
- `test/history.test.ts:410` — history date markers survive a retry and a fresh worker without duplicate date posts.
- `test/history.test.ts:420` — skipped and revoked messages do not produce empty date sections.
- `test/history.test.ts:431` — member DM date markers use its bot and compact imported messages still identify the owner.
- `test/history.test.ts:441` — older in-progress history runs keep their legacy presentation and do not gain date markers mid-import.
- `test/history.test.ts:450` — every history choice deletes only its own prompt and duplicate clicks cannot schedule more deletion.
- `test/history.test.ts:463` — prompt-deletion network failures retry independently without blocking an import.
- `test/history.test.ts:474` — group history uses each present member bot while Bifrost handles absent/unbound authors, own messages and dates.

### http.test.ts

- `test/http.test.ts:14` — Telegram refusals expose safe reason codes without logging provider descriptions.
- `test/http.test.ts:26` — real HTTP ingress authenticates raw OpenWA bytes, Telegram secrets and signed management actions.
- `test/http.test.ts:47` — real API transport preserves JSON, quotes, idempotency and handles rate limits without leaking tokens.
- `test/http.test.ts:70` — real multipart transport preserves attachment bytes and caption entities.
- `test/http.test.ts:89` — Telegram caption edits satisfy the OpenWA HTTP contract and a refused edit cannot stall later sends.
- `test/http.test.ts:130` — edit transport keeps known rate refusals retryable and ambiguous mutations uncertain.

### identity.test.ts

- `test/identity.test.ts:13` — legacy database keeps its original state identity after restart with new OpenWA credentials.
- `test/identity.test.ts:42` — explicit permanent identity is independent of OpenWA and a conflicting identity is refused.
- `test/identity.test.ts:51` — new webhook transport rejects the old session and cannot override the permanent account.
- `test/identity.test.ts:68` — active history still holds new and legacy live jobs after the OpenWA session changes.
- `test/identity.test.ts:81` — uncertain outgoing sends remain blocked when the API key and session are replaced.

### mentions.test.ts

- `test/mentions.test.ts:49` — incoming WhatsApp tags become readable native bot mentions with correct UTF-16 offsets and no duplicate sender header.
- `test/mentions.test.ts:59` — absent bots link to Telegram, unbound members link to WhatsApp, and hidden numbers use the uniform fallback.
- `test/mentions.test.ts:66` — mention metadata and phone/LID aliases determine replacements, while ambiguous or literal numbers stay unchanged.
- `test/mentions.test.ts:74` — incoming captions, replies and edits retain mentions, and explicit removal clears them.
- `test/mentions.test.ts:88` — manual history renders mention entities alongside native dates and keeps source mappings.
- `test/mentions.test.ts:98` — incoming mention rendering remains stable across retries and text splitting preserves offsets.
- `test/mentions.test.ts:107` — outgoing bot usernames and name entities produce one native WhatsApp mention and preserve quoted replies.
- `test/mentions.test.ts:115` — emails, URLs, code and unrelated numbers do not become outgoing mentions.
- `test/mentions.test.ts:123` — LID-bound bots and copied unbound tags send their exact WhatsApp identities.
- `test/mentions.test.ts:131` — outgoing captions and edits carry updated mention lists, including explicit empty removal.
- `test/mentions.test.ts:144` — invalid, wrong-group, previous-session and departed-member tokens are refused before sending.
- `test/mentions.test.ts:155` — copy tokens persist across reopening and renaming, and duplicate names get distinct identities.
- `test/mentions.test.ts:165` — /mention searches names and usernames, gives copy buttons, and never writes to WhatsApp.
- `test/mentions.test.ts:175` — picker uses scoped pagination and rejects wrong owner, bot, topic, session and expired callbacks.
- `test/mentions.test.ts:192` — stale-roster refresh failure shows a cached picker but refuses unvalidated sends.
- `test/mentions.test.ts:199` — 429 retries retain the original resolved text and identity after bot bindings change.
- `test/mentions.test.ts:207` — group member bots ignore ordinary tagged updates so Bifrost sends exactly once.
- `test/mentions.test.ts:214` — incomplete copied tokens are refused and fresh roster caches expire after five minutes.
- `test/mentions.test.ts:225` — picker tokens handle short-ID collisions and aliases learned after copying.
- `test/mentions.test.ts:234` — picker accepts a linked management topic, isolates its callbacks and rejects unrelated DMs and owners.

### reactions.test.ts

- `test/reactions.test.ts:45` — reactions to missing messages and notices targeting deleted topics do not block later deliveries.
- `test/reactions.test.ts:57` — WhatsApp reactions appear natively as the reactor’s available member bot without a reply notice.
- `test/reactions.test.ts:64` — removing one member’s reaction clears only that bot’s reaction.
- `test/reactions.test.ts:70` — Bifrost shows the latest unbound reaction and preserves remaining reactors when one removes theirs.
- `test/reactions.test.ts:78` — full OpenWA reaction snapshots and LID aliases reconstruct every actor and remove missing reactions.
- `test/reactions.test.ts:86` — private message IDs remain scoped to their original bot, regardless of who reacted in WhatsApp.
- `test/reactions.test.ts:93` — split messages react on the attachment once and skip Telegram copies already cleared.
- `test/reactions.test.ts:101` — a member bot absent from the group falls back to Bifrost.
- `test/reactions.test.ts:106` — unavailable emoji clears a stale native reaction and uses one attributed fallback in the same topic.
- `test/reactions.test.ts:118` — a rate-limited fallback notice is not lost when the original reaction event retries.
- `test/reactions.test.ts:125` — ambiguous native updates safely retry without uncertain outgoing-send effects.
- `test/reactions.test.ts:134` — Telegram-owner reactions echoing from WhatsApp do not add duplicate bot reactions or loop back.
- `test/reactions.test.ts:144` — later repeats of a reaction already represented by the Telegram owner remain suppressed.
- `test/reactions.test.ts:155` — WhatsApp heart variations normalize to Telegram emoji and do not create a second reaction.
- `test/reactions.test.ts:162` — unsupported Telegram custom reactions do not clear an existing WhatsApp reaction.
- `test/reactions.test.ts:168` — Bifrost maps laughing reactions on owner messages, deduplicates equivalent changes and clears on removal.
- `test/reactions.test.ts:178` — equivalent reactions work through member bots and private copies without altering supported emoji.
- `test/reactions.test.ts:186` — a refused equivalent and an unsupported unmapped emoji both keep the original in text fallbacks.
- `test/reactions.test.ts:195` — Telegram-owner reaction echoes stay suppressed after adding equivalent rendering.
- `test/reactions.test.ts:204` — Telegram custom reactions forward their Unicode equivalent and cache it without converting it to Telegram approximations.
- `test/reactions.test.ts:214` — member DM reactions are admitted with bot-scoped mappings while group duplicates are ignored.
- `test/reactions.test.ts:223` — adding a custom reaction beside an existing reaction forwards the new one; removal keeps the remaining one.
- `test/reactions.test.ts:235` — unresolvable custom reactions produce a temporary independent notice without clearing WhatsApp or blocking a chat.
- `test/reactions.test.ts:244` — a member unable to access another bot’s message falls back to Bifrost and cached refusals do not suppress that fallback.
- `test/reactions.test.ts:259` — old definitive reaction refusals expire so corrected permissions can recover.
- `test/reactions.test.ts:269` — custom-emoji resolution and outbound payload stay frozen across a known send retry.

### revocations.test.ts

- `test/revocations.test.ts:62` — a deleted owner message gets Delete/Keep without overwriting or deleting its Telegram content.
- `test/revocations.test.ts:73` — Keep removes the fallback notice and preserves the original, including after late edits/history.
- `test/revocations.test.ts:86` — own phone-originated messages get inline choices; Keep and expiration remove only the buttons.
- `test/revocations.test.ts:97` — Delete removes every text/media part in the chosen destination while preserving other copies and mappings.
- `test/revocations.test.ts:108` — other people’s deleted text/media copies are removed automatically without a replacement or choices.
- `test/revocations.test.ts:115` — duplicate events and conflicting clicks cannot reopen or reverse a completed choice.
- `test/revocations.test.ts:127` — choices validate owner, bot, destination, message, session and expiry before changing content.
- `test/revocations.test.ts:140` — expiration defaults to keeping the message and never deletes the original.
- `test/revocations.test.ts:148` — member-bot private copies keep their bot-scoped identity even when Telegram message IDs overlap.
- `test/revocations.test.ts:158` — a Telegram deletion refusal preserves the remaining copy and produces no destructive content replacement.
- `test/revocations.test.ts:168` — an ambiguous keyboard update safely retries without pausing as an uncertain send.
- `test/revocations.test.ts:178` — saved WhatsApp phone events identify the owner for messages predating the fromMe metadata.
- `test/revocations.test.ts:186` — fallback prompt expiration keeps the owner’s original message.
- `test/revocations.test.ts:193` — a timed-out automatic deletion retries safely without an uncertain message send.
- `test/revocations.test.ts:202` — a timed-out callback acknowledgement retries without blocking the conversation as uncertain.

### setup.test.ts

- `test/setup.test.ts:32` — setup accepts OpenWA ready, migrates before local Telegram calls and installs both webhooks.
- `test/setup.test.ts:42` — setup refuses incomplete or disconnected sessions before migrating or registering callbacks.

### statuses.test.ts

- `test/statuses.test.ts:60` — Status webhooks share one archive topic with linked authors and native times, without new conversations.
- `test/statuses.test.ts:79` — startup upgrades only bridge callback events, imports available stored Status posts and deduplicates webhook races.
- `test/statuses.test.ts:98` — a fresh or wildcard webhook installation remains idempotent.
- `test/statuses.test.ts:108` — image, video and voice Status posts use native media with attributed captions in the feed.
- `test/statuses.test.ts:123` — Status media uses the authenticated local status endpoint and never an advertised media URL.
- `test/statuses.test.ts:132` — omitted or expired media keeps its caption and a readable unavailable notice.
- `test/statuses.test.ts:143` — expired and malformed incoming stories are skipped while already archived posts remain.
- `test/statuses.test.ts:156` — typing, edits, message actions and reactions in the Status topic cannot publish or send WhatsApp messages.
- `test/statuses.test.ts:172` — /statuses opens and refreshes the feed from Bifrost DM; /status remains the health command.
- `test/statuses.test.ts:187` — replying /mute in Status suppresses only that author before media download, preserving chats and archived posts.
- `test/statuses.test.ts:206` — refresh honors mutes and unmuting resumes new posts without replaying stories skipped while muted.
- `test/statuses.test.ts:222` — muted-person preferences survive reopening SQLite and resolve aliases with the latest preference winning.
- `test/statuses.test.ts:239` — muted list supports one-tap unmute and pagination without changing other people’s preferences.
- `test/statuses.test.ts:260` — mute controls enforce owner, bot, session, chat, topic and expiry; stale buttons cannot undo a later mute.
- `test/statuses.test.ts:284` — retrying a mute confirmation or unmute callback cannot reverse a newer decision from another chat.
- `test/statuses.test.ts:299` — Status mute commands require a mapped Status reply and ordinary conversation mute keeps its existing behavior.
- `test/statuses.test.ts:311` — Status topic clear log preserves posts, while clear all removes only this topic’s recorded posts.

### stickers.test.ts


### sync.test.ts

- `test/sync.test.ts:67` — sync updates group metadata, existing member bots and missing-bot buttons without WhatsApp writes.
- `test/sync.test.ts:84` — an inbox group only renames its topic and never overwrites the shared management group.
- `test/sync.test.ts:92` — content-hash picture caching avoids repeated uploads even when signed source URLs rotate.
- `test/sync.test.ts:108` — hidden pictures and unavailable descriptions preserve Telegram data.
- `test/sync.test.ts:117` — incomplete metadata and failed lookups cannot clear the old roster or group identity.
- `test/sync.test.ts:129` — missing group permission yields a scoped grant button while contact metadata can still sync.
- `test/sync.test.ts:143` — one contact bot shared across groups is fetched once and LID aliases retain its binding.
- `test/sync.test.ts:156` — /sync scopes to its conversation, and global sync uses the local management queue.
- `test/sync.test.ts:172` — /sync in a bound member bot DM works without a one-to-one route and reports back through that bot.
- `test/sync.test.ts:195` — an unbound member bot explains the missing binding for Start and /sync without accessing WhatsApp.
- `test/sync.test.ts:209` — member-bot DM sync keeps the contact scope with an existing inbox topic and rejects global sync.
- `test/sync.test.ts:225` — a failed group lookup does not prevent existing contact bots from syncing.
- `test/sync.test.ts:235` — chat jobs take priority over metadata work, and coalesced manual requests receive a completion.
- `test/sync.test.ts:248` — cron is timezone-aware and persists one run per scheduled minute across restarts.
- `test/sync.test.ts:258` — cron supports quarter-hour, daily, weekday and disabled schedules and rejects malformed fields.

### transfer.test.ts

- `test/transfer.test.ts:9` — import keeps credentials literal, normalizes only local paths and removes the retired n8n setting.
- `test/transfer.test.ts:18` — export discovers legacy and Bifrost named volumes and refuses ambiguous or bind-mounted state.

### unmirror.test.ts

- `test/unmirror.test.ts:34` — /unmirror queues a durable move and restores the topic controls without deleting archived messages.
- `test/unmirror.test.ts:61` — new WhatsApp messages and Telegram replies use management while the former mirror stops sending.
- `test/unmirror.test.ts:81` — notification retries reuse the created topic and leave uncertain deliveries intact.
- `test/unmirror.test.ts:96` — topic permission errors keep the existing mirror and explain how to retry.
- `test/unmirror.test.ts:105` — uncertain topic creation is preserved for review without replaying the creation.
- `test/unmirror.test.ts:113` — muted and ignored mirrors retain their setting after returning to management.
- `test/unmirror.test.ts:123` — an active history import finishes before the mirror can be moved.
- `test/unmirror.test.ts:132` — stale move requests and notification retries cannot undo later routing choices.
- `test/unmirror.test.ts:146` — /unmirror requires the owner, master bot and a linked WhatsApp mirror group.
