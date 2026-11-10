# Behavior evidence and coverage

Baseline: `checkpoint-go-contract-baseline`. Specs describe current behavior; the owner has approved preserving it for Go, except for conditional caption clearing recorded in the change specs.

Evidence levels: **regression** means local fixture tests; **inspection** means source/config inspection; **external verification pending** means real providers, Docker transfer or codec dependencies have not been exercised. Fixture success does not prove production provider compatibility.

Validation: Bun 1.4.2, `bun run test`: **263 passed, 11 skipped, 0 failed**. Loopback HTTP access was required; the initial sandbox run failed 30 HTTP/media tests because binding was unavailable. Docker archive tests and codec fixtures remain opt-in.

## Capability map

| Capability | Source | Regression files |
| --- | --- | --- |
| [conversation-routing](specs/conversation-routing/spec.md) | [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts) | [bridge](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts), [group-picker](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/group-picker.test.ts), [unmirror](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/unmirror.test.ts) |
| [message-relay](specs/message-relay/spec.md) | [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts) | [bridge](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts), [format](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts), [http](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/http.test.ts) |
| [mentions](specs/mentions/spec.md) | [src/mentions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/mentions.ts), [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts) | [mentions](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts) |
| [media](specs/media/spec.md) | [src/media.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/media.ts), [src/stickers.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/stickers.ts), [src/sticker-worker.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/sticker-worker.ts) | [media](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/media.test.ts), [stickers](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/stickers.test.ts), [bridge](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts) |
| [history-import](specs/history-import/spec.md) | [src/history.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/history.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts) | [history](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts), [identity](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/identity.test.ts) |
| [reactions-and-deletions](specs/reactions-and-deletions/spec.md) | [src/reactions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/reactions.ts), [src/revocations.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/revocations.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts) | [reactions](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts), [revocations](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts) |
| [status-feed](specs/status-feed/spec.md) | [src/statuses.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/statuses.ts), [src/openwa.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/openwa.ts) | [statuses](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts) |
| [owner-controls](specs/owner-controls/spec.md) | [src/control.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/control.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts) | [control](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts), [group-picker](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/group-picker.test.ts) |
| [metadata-sync](specs/metadata-sync/spec.md) | [src/sync.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/sync.ts), [src/cron.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/cron.ts) | [sync](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts) |
| [ingress-and-delivery](specs/ingress-and-delivery/spec.md) | [src/server.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/server.ts), [src/api.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/api.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts) | [http](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/http.test.ts), [bridge](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts), [identity](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/identity.test.ts) |
| [identity-and-operations](specs/identity-and-operations/spec.md) | [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/config.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/config.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [scripts/configure.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/configure.ts), [scripts/setup.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/setup.ts), [scripts/transfer.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/transfer.ts) | [identity](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/identity.test.ts), [configure](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/configure.test.ts), [setup](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/setup.test.ts), [transfer](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/transfer.test.ts) |

## Requirement-to-evidence map

Each requirement maps to the source and regression files listed in its capability; named cases below narrow the evidence. Operational defaults and the full HTTP surface also depend on source inspection. No live-provider verification has been performed.

### conversation-routing

- **Inbox creation** — [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts); fixtures: bridge, group-picker, unmirror.
- **Member bot identity** — [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts); fixtures: bridge, group-picker, unmirror.
- **Private route activation** — [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts); fixtures: bridge, group-picker, unmirror.
- **Group linking** — [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts); fixtures: bridge, group-picker, unmirror.
- **Inbox retirement** — [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts); fixtures: bridge, group-picker, unmirror.
- **Return to management** — [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts); fixtures: bridge, group-picker, unmirror.
- **Route recovery** — [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts); fixtures: bridge, group-picker, unmirror.
### message-relay

- **Owner sends and echo suppression** — [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts); fixtures: bridge, format, http.
- **Quote isolation** — [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts); fixtures: bridge, format, http.
- **Sender presentation** — [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts); fixtures: bridge, format, http.
- **Safe formatting** — [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts); fixtures: bridge, format, http.
- **Telegram edits** — [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts); fixtures: bridge, format, http.
- **WhatsApp edits** — [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts); fixtures: bridge, format, http.
- **Structured messages** — [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts); fixtures: bridge, format, http.
- **Read receipts** — [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts); fixtures: bridge, format, http.
### mentions

- **Incoming mention rendering** — [src/mentions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/mentions.ts), [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts); fixtures: mentions.
- **Outgoing mentions** — [src/mentions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/mentions.ts), [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts); fixtures: mentions.
- **Mention picker** — [src/mentions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/mentions.ts), [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts); fixtures: mentions.
- **Roster validation** — [src/mentions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/mentions.ts), [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts); fixtures: mentions.
- **Retry stability** — [src/mentions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/mentions.ts), [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/format.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/format.ts); fixtures: mentions.
### media

- **Native attachments** — [src/media.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/media.ts), [src/stickers.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/stickers.ts), [src/sticker-worker.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/sticker-worker.ts); fixtures: media, stickers, bridge.
- **Retrieval precedence** — [src/media.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/media.ts), [src/stickers.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/stickers.ts), [src/sticker-worker.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/sticker-worker.ts); fixtures: media, stickers, bridge.
- **Unavailable attachments** — [src/media.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/media.ts), [src/stickers.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/stickers.ts), [src/sticker-worker.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/sticker-worker.ts); fixtures: media, stickers, bridge.
- **Bounded downloads** — [src/media.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/media.ts), [src/stickers.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/stickers.ts), [src/sticker-worker.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/sticker-worker.ts); fixtures: media, stickers, bridge.
- **Sticker conversion** — [src/media.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/media.ts), [src/stickers.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/stickers.ts), [src/sticker-worker.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/sticker-worker.ts); fixtures: media, stickers, bridge.
- **Temporary media cleanup** — [src/media.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/media.ts), [src/stickers.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/stickers.ts), [src/sticker-worker.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/sticker-worker.ts); fixtures: media, stickers, bridge.
### history-import

- **Explicit history choice** — [src/history.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/history.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: history, identity.
- **Choice authorization** — [src/history.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/history.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: history, identity.
- **Complete ordered batch** — [src/history.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/history.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: history, identity.
- **Destination receipts** — [src/history.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/history.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: history, identity.
- **History quotes** — [src/history.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/history.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: history, identity.
- **Restartable import** — [src/history.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/history.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: history, identity.
- **History presentation** — [src/history.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/history.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: history, identity.
- **Prompt cleanup** — [src/history.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/history.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: history, identity.
### reactions-and-deletions

- **Native reactions** — [src/reactions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/reactions.ts), [src/revocations.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/revocations.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: reactions, revocations.
- **Reaction mapping** — [src/reactions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/reactions.ts), [src/revocations.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/revocations.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: reactions, revocations.
- **Owner reactions** — [src/reactions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/reactions.ts), [src/revocations.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/revocations.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: reactions, revocations.
- **Reaction retries** — [src/reactions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/reactions.ts), [src/revocations.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/revocations.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: reactions, revocations.
- **Other-author revocation** — [src/reactions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/reactions.ts), [src/revocations.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/revocations.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: reactions, revocations.
- **Owner revocation choice** — [src/reactions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/reactions.ts), [src/revocations.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/revocations.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: reactions, revocations.
- **Revocation authorization** — [src/reactions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/reactions.ts), [src/revocations.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/revocations.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: reactions, revocations.
- **Explicit source actions** — [src/reactions.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/reactions.ts), [src/revocations.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/revocations.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: reactions, revocations.
### status-feed

- **Shared Status archive** — [src/statuses.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/statuses.ts), [src/openwa.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/openwa.ts); fixtures: statuses.
- **Status content** — [src/statuses.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/statuses.ts), [src/openwa.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/openwa.ts); fixtures: statuses.
- **Passive Status feed** — [src/statuses.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/statuses.ts), [src/openwa.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/openwa.ts); fixtures: statuses.
- **Independent Status mutes** — [src/statuses.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/statuses.ts), [src/openwa.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/openwa.ts); fixtures: statuses.
- **Status control ordering** — [src/statuses.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/statuses.ts), [src/openwa.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/openwa.ts); fixtures: statuses.
### owner-controls

- **Command addressing** — [src/control.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/control.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: control, group-picker.
- **Conversation preferences** — [src/control.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/control.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: control, group-picker.
- **Scoped callbacks** — [src/control.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/control.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: control, group-picker.
- **Durable notice expiry** — [src/control.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/control.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: control, group-picker.
- **Clear notices** — [src/control.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/control.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: control, group-picker.
- **Clear management archive** — [src/control.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/control.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: control, group-picker.
- **Health and menus** — [src/control.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/control.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: control, group-picker.
### metadata-sync

- **Scoped metadata refresh** — [src/sync.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/sync.ts), [src/cron.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/cron.ts); fixtures: sync.
- **Metadata preservation** — [src/sync.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/sync.ts), [src/cron.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/cron.ts); fixtures: sync.
- **Efficient metadata work** — [src/sync.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/sync.ts), [src/cron.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/cron.ts); fixtures: sync.
- **No source mutations** — [src/sync.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/sync.ts), [src/cron.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/cron.ts); fixtures: sync.
- **Persistent scheduling** — [src/sync.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/sync.ts), [src/cron.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/cron.ts); fixtures: sync.
### ingress-and-delivery

- **HTTP surface** — [src/server.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/server.ts), [src/api.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/api.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: http, bridge, identity.
- **Ingress authentication** — [src/server.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/server.ts), [src/api.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/api.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: http, bridge, identity.
- **Payload validation** — [src/server.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/server.ts), [src/api.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/api.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: http, bridge, identity.
- **Owner admission** — [src/server.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/server.ts), [src/api.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/api.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: http, bridge, identity.
- **Durable deduplication** — [src/server.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/server.ts), [src/api.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/api.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: http, bridge, identity.
- **Stable external effects** — [src/server.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/server.ts), [src/api.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/api.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: http, bridge, identity.
- **Known failures versus ambiguity** — [src/server.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/server.ts), [src/api.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/api.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: http, bridge, identity.
- **Restart recovery** — [src/server.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/server.ts), [src/api.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/api.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: http, bridge, identity.
- **Explicit review** — [src/server.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/server.ts), [src/api.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/api.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: http, bridge, identity.
- **Safe error reporting** — [src/server.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/server.ts), [src/api.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/api.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [src/bridge.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); fixtures: http, bridge, identity.
### identity-and-operations

- **Permanent account identity** — [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/config.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/config.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [scripts/configure.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/configure.ts), [scripts/setup.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/setup.ts), [scripts/transfer.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/transfer.ts); fixtures: identity, configure, setup, transfer.
- **Protected configuration** — [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/config.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/config.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [scripts/configure.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/configure.ts), [scripts/setup.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/setup.ts), [scripts/transfer.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/transfer.ts); fixtures: identity, configure, setup, transfer.
- **Configuration validation** — [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/config.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/config.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [scripts/configure.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/configure.ts), [scripts/setup.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/setup.ts), [scripts/transfer.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/transfer.ts); fixtures: identity, configure, setup, transfer.
- **Setup readiness** — [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/config.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/config.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [scripts/configure.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/configure.ts), [scripts/setup.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/setup.ts), [scripts/transfer.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/transfer.ts); fixtures: identity, configure, setup, transfer.
- **Webhook subscriptions** — [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/config.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/config.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [scripts/configure.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/configure.ts), [scripts/setup.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/setup.ts), [scripts/transfer.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/transfer.ts); fixtures: identity, configure, setup, transfer.
- **Session notices** — [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/config.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/config.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [scripts/configure.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/configure.ts), [scripts/setup.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/setup.ts), [scripts/transfer.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/transfer.ts); fixtures: identity, configure, setup, transfer.
- **Transfer preservation** — [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/config.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/config.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [scripts/configure.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/configure.ts), [scripts/setup.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/setup.ts), [scripts/transfer.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/transfer.ts); fixtures: identity, configure, setup, transfer.
- **Archive validation** — [src/identity.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/identity.ts), [src/config.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/config.ts), [src/store.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/store.ts), [scripts/configure.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/configure.ts), [scripts/setup.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/setup.ts), [scripts/transfer.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/scripts/transfer.ts); fixtures: identity, configure, setup, transfer.

## bridge.test.ts

- [standalone management and legacy forwarding jobs converge on one durable action without HTTP forwarding](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L45)
- [a group-only contact bot creates its direct route without a management topic and sends ordinary and quoted replies](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L57)
- [unbound member DMs explain the missing binding instead of silently dropping the message](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L71)
- [phone-originated private messages stay in their started member-bot DM and can be quoted by incoming replies](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L78)
- [a deleted inbox topic is recreated after a definitive pre-delivery refusal and stale quotes are omitted](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L89)
- [an old frozen master target can recover into the ready contact DM only before any delivery](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L101)
- [a rejected media relay moves to the ready member DM without trying document fallback in the missing topic](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L117)
- [a blocked member can fall back even when its management topic was deleted](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L134)
- [edits of Telegram copies explicitly cleared from the archive do not recreate them](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L150)
- [unknown conversation gets one topic; replay is deduplicated and never marks read](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L159)
- [dedicated bot wins in a group only when present; master falls back with sender name](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L169)
- [member-bot media, stickers, locations, cards and polls omit redundant sender headers](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L183)
- [member-bot history keeps a compact time and body while phone-originated sends keep owner attribution](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L200)
- [member-bot edits do not restore a sender header; master edit fallback remains attributed](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L214)
- [private member-bot messages and edit fallbacks stay nameless in the correct bot DM](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L227)
- [Telegram reply maps to WhatsApp quote; send echoes and overlapping bot updates do not loop](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L239)
- [/create in a linked whole group accepts Telegram reply-thread IDs and selects the replied-to participant](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L256)
- [ordinary replies in a whole group share one queue stream and retain WhatsApp quotes](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L280)
- [configured inbox topics still require an exact topic match](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L295)
- [whole-group reply-thread handling still rejects a quoted message from another WhatsApp conversation](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L309)
- [uncertain remote send pauses a conversation, while known 429 retries](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L318)
- [sealed tokens authenticate and are absent from SQLite plaintext](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L329)
- [management signatures reject forgery and repeated actions are durable](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L336)
- [out-of-order WhatsApp revocation uses revokedId and prevents history resurrection](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L343)
- [phone/LID aliases share a contact bot across conversations](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L350)
- [each WhatsApp media class uses native Telegram methods; view-once content exposed by OpenWA is archived](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L358)
- [converted WhatsApp stickers use WebM filenames and preserve quote mappings across a partial retry](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L368)
- [sticker conversion failures and definitive Telegram rejections send the untouched original document](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L384)
- [outgoing TGS and WebM stickers use converted WebP with replies and the original delivery key](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L400)
- [ordinary documents bypass conversion and failed outgoing sticker conversions keep original bytes](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L419)
- [rate-limited sticker sends freeze the conversion choice and bytes even after a converter recovers](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L435)
- [missing media is visible, and location/contact/poll use structured representations](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L452)
- [dedicated private bot waits for Start, then routes private replies to the contact](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L465)
- [muted conversations can resume; creating a bot at capacity leaves master routing intact](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L478)
- [429 is retried with the same WhatsApp idempotency key](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L491)
- [child-bot group callbacks remain actionable while its ordinary group updates are ignored](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L503)
- [private message ids are isolated between contact bots](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L509)
- [a partial sticker delivery retries the attachment without losing it or duplicating the label](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L523)
- [blocked dedicated bot falls back to its management topic on a definitive Telegram refusal](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L539)
- [completed external effects retain their original destination when routing changes during a retry](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L552)
- [history is bounded and enqueued with historical markers](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L564)
- [opening a second store does not reset a live process; explicit recovery preserves uncertain sends](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts#L573)

## configure.test.ts

- [fresh configuration generates independent private secrets from the tracked template](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/configure.test.ts#L8)
- [reconfiguration preserves existing encryption keys and all operator configuration](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/configure.test.ts#L21)

## control.test.ts

- [Bifrost ignores commands for another bot, while the addressed member bot handles its own group Start](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts#L43)
- [join, leave, title and forum service events produce no unsupported-message replies or WhatsApp operations](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts#L56)
- [edited commands and edits of former commands never become WhatsApp messages](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts#L65)
- [commands and notices expire durably, while WhatsApp text and poll relays remain ordinary chat messages](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts#L74)
- [interactive command replies get a longer lifetime](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts#L92)
- [observing a member-bot notice through Bifrost keeps its expiration and owner-only execution checks](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts#L99)
- [/clear log removes local notices and command messages, preserving chat messages, bindings and mappings](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts#L109)
- [/clear all in management General preserves other topics and old/protected messages](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts#L123)
- [both clear-all forms clear only the current management topic, preserving other chats, later messages and mappings](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts#L138)
- [clear all remains unavailable in mirror groups, private chats and through member bots](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts#L161)
- [cleanup isolates private message IDs for each member bot](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts#L171)
- [one undeletable message does not prevent removal of other notices](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts#L181)
- [ambiguous cleanup failures retry as idempotent deletes without uncertain delivery effects](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts#L191)
- [startup registers command menus for migrated master/member bots, including management-topic clear_all](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts#L202)
- [existing recorded chat/history is indexed for cleanup without expiring mirrored conversations](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/control.test.ts#L220)

## format.test.ts

- [master relay uses a bold WhatsApp-linked name and body on one line](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L48)
- [verified phone JIDs open their chat; unknown identities use the generic WhatsApp launch link](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L54)
- [untrusted HTML characters remain literal text and cannot inject markup or links](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L62)
- [compact history uses linked sender and bracketed native time while member bots omit their names](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L71)
- [invalid source timestamps remain clearly historical without inventing a date](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L84)
- [compact clocks handle invalid timestamps and dates group in the configured timezone](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L90)
- [saved history presentations retain legacy rendering when compact style was not selected](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L103)
- [formatted splitting preserves every UTF-16 character, clips links safely and keeps native time atomic](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L109)
- [caption entity offsets are safe and oversize raw captions must be handled as text](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L119)
- [relay transport carries native entities and disables the generated WhatsApp header preview](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L125)
- [sender links resolve known LID aliases rather than treating a LID as a phone number](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L135)
- [phone-originated private messages and edits never link You to the conversation partner](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L141)
- [You links only to the independently verified session phone](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L152)
- [long media captions keep all content in formatted text and a compact linked attachment header](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L161)
- [history metadata survives formatted message edits and captions do not contain raw HTML](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L173)
- [formatted packets remain immutable across a partial send retry](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L183)
- [presentation stays frozen while a media upload retries after a contact rename](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L191)
- [pre-upgrade partial deliveries retain their legacy packet boundaries](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/format.test.ts#L201)

## group-picker.test.ts

- [native group creation/selection grants master rights and links only after the owner shares a group](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/group-picker.test.ts#L32)
- [expired and previous-session buttons produce guidance without opening a selector](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/group-picker.test.ts#L55)
- [unknown requests and other Telegram users cannot link a group](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/group-picker.test.ts#L68)
- [picker rejects public destinations and the management group](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/group-picker.test.ts#L78)
- [new group inbox controls include the button and /group works for existing topics](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/group-picker.test.ts#L88)

## history.test.ts

- [queued history resumes under its original checkpoint after OpenWA credentials change](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L55)
- [an unexpired history choice remains usable after an OpenWA session rotation](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L70)
- [/history n fetches the complete batch first, posts oldest first, and recreates native reply chains](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L82)
- [history delivered to management can be imported into a mirror and repeated requests do not duplicate it](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L96)
- [history in a new member DM includes own messages and preserves bot-scoped reply targets](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L110)
- [history retries use the saved batch, preserve earlier parts and hold only live events for this conversation](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L121)
- [history orders parents before children even with equal or inconsistent source timestamps and previews outside-window quotes](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L140)
- [history reuses archived Telegram media even after its management topic has been removed](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L148)
- [manual history can restore a copy explicitly cleared from the current destination without reusing deleted receipts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L163)
- [history sends retained WebM sticker media through the Telegram sticker preparation path](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L171)
- [history validates counts and uses the gateway deep-history option above 100](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L189)
- [history fetch failure releases live delivery and never deletes a management topic](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L197)
- [linking a mirror queues old-topic deletion but never imports history automatically](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L205)
- [starting a bound DM retires its inbox separately and cleanup refusal does not block messaging](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L217)
- [retiring a DM topic is durable and a blocked member recreates an inbox lazily](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L230)
- [General and management topics shared with another conversation cannot be deleted by retirement](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L242)
- [a retired direct route cannot capture General or another management topic](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L251)
- [history requests from a previous session cannot block the current session](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L260)
- [/history waits for a choice without fetching or holding live messages, and Cancel does not start an import](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L267)
- [Import all restores manually cleared messages despite old delivery records and links replies to the new copies](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L281)
- [Import all previews out-of-window quotes instead of replying to stale copies from a cleared chat](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L301)
- [Import all retries a frozen batch without duplicating parts and repeated clicks cannot start another run or switch mode](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L310)
- [Import all still excludes WhatsApp-revoked messages and preserves other destinations](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L327)
- [history choices validate owner, bot, session, destination, topic, binding and expiry](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L342)
- [member DM Import all remains bot scoped and legacy Recent history buttons now show the choice](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L360)
- [two pending choices cannot start concurrent imports or change an active import mode](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L377)
- [reactions prefer a fresh unavailable-media placeholder over a stale attachment from before clearing](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L385)
- [media import survives API/session rotation and retries without replaying completed attachments](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L399)
- [a genuine source-media miss retains its caption and completes a history receipt](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L436)
- [history posts one retained date marker per day with time-only messages and replies across midnight](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L465)
- [history date markers survive a retry and a fresh worker without duplicate date posts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L478)
- [skipped and revoked messages do not produce empty date sections](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L488)
- [member DM date markers use its bot and compact imported messages still identify the owner](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L499)
- [older in-progress history runs keep their legacy presentation and do not gain date markers mid-import](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L509)
- [every history choice deletes only its own prompt and duplicate clicks cannot schedule more deletion](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L518)
- [prompt-deletion network failures retry independently without blocking an import](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L531)
- [group history uses each present member bot while Bifrost handles absent/unbound authors, own messages and dates](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/history.test.ts#L542)

## http.test.ts

- [Telegram refusals expose safe reason codes without logging provider descriptions](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/http.test.ts#L14)
- [real HTTP ingress authenticates raw OpenWA bytes, Telegram secrets and signed management actions](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/http.test.ts#L26)
- [real API transport preserves JSON, quotes, idempotency and handles rate limits without leaking tokens](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/http.test.ts#L47)
- [real multipart transport preserves attachment bytes and caption entities](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/http.test.ts#L70)
- [Telegram caption edits satisfy the OpenWA HTTP contract and a refused edit cannot stall later sends](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/http.test.ts#L89)
- [edit transport keeps known rate refusals retryable and ambiguous mutations uncertain](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/http.test.ts#L130)

## identity.test.ts

- [legacy database keeps its original state identity after restart with new OpenWA credentials](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/identity.test.ts#L13)
- [explicit permanent identity is independent of OpenWA and a conflicting identity is refused](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/identity.test.ts#L42)
- [new webhook transport rejects the old session and cannot override the permanent account](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/identity.test.ts#L51)
- [active history still holds new and legacy live jobs after the OpenWA session changes](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/identity.test.ts#L68)
- [uncertain outgoing sends remain blocked when the API key and session are replaced](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/identity.test.ts#L81)

## media.test.ts

- [archive and inline reuse avoid a WhatsApp source request](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/media.test.ts#L36)
- [only a documented cache miss triggers source download; source absence remains distinct](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/media.test.ts#L47)
- [document MIME metadata survives OpenWA’s inert Content-Type and disabled downloads remain explicit](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/media.test.ts#L69)
- [legacy cache misses work but a missing download route is retryable, without declaring source unavailable](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/media.test.ts#L82)
- [source rate limits remain retryable and the next attempt can finish](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/media.test.ts#L90)
- [cancelled source transfer removes incomplete bytes and remains retryable](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/media.test.ts#L111)

## mentions.test.ts

- [incoming WhatsApp tags become readable native bot mentions with correct UTF-16 offsets and no duplicate sender header](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L49)
- [absent bots link to Telegram, unbound members link to WhatsApp, and hidden numbers use the uniform fallback](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L59)
- [mention metadata and phone/LID aliases determine replacements, while ambiguous or literal numbers stay unchanged](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L66)
- [incoming captions, replies and edits retain mentions, and explicit removal clears them](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L74)
- [manual history renders mention entities alongside native dates and keeps source mappings](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L88)
- [saved history WID objects resolve names and native mention entities without fetching history again](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L98)
- [serialized mention IDs and string IDs share frozen rendering, while malformed IDs never authorize replacements](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L117)
- [device contact placeholders do not hide cached names or a registered bot for the same person](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L126)
- [a named roster entry takes precedence over identifier placeholders when no contact has a display name](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L137)
- [incoming mention rendering remains stable across retries and text splitting preserves offsets](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L144)
- [outgoing bot usernames and name entities produce one native WhatsApp mention and preserve quoted replies](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L153)
- [emails, URLs, code and unrelated numbers do not become outgoing mentions](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L161)
- [LID-bound bots and copied unbound tags send their exact WhatsApp identities](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L169)
- [outgoing captions and edits carry updated mention lists, including explicit empty removal](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L177)
- [invalid, wrong-group, previous-session and departed-member tokens are refused before sending](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L190)
- [copy tokens persist across reopening and renaming, and duplicate names get distinct identities](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L201)
- [/mention searches names and usernames, gives copy buttons, and never writes to WhatsApp](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L211)
- [picker uses scoped pagination and rejects wrong owner, bot, topic, session and expired callbacks](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L221)
- [stale-roster refresh failure shows a cached picker but refuses unvalidated sends](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L238)
- [429 retries retain the original resolved text and identity after bot bindings change](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L245)
- [group member bots ignore ordinary tagged updates so Bifrost sends exactly once](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L253)
- [incomplete copied tokens are refused and fresh roster caches expire after five minutes](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L260)
- [picker tokens handle short-ID collisions and aliases learned after copying](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L271)
- [picker accepts a linked management topic, isolates its callbacks and rejects unrelated DMs and owners](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/mentions.test.ts#L280)

## reactions.test.ts

- [reactions to missing messages and notices targeting deleted topics do not block later deliveries](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L45)
- [WhatsApp reactions appear natively as the reactor’s available member bot without a reply notice](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L57)
- [removing one member’s reaction clears only that bot’s reaction](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L64)
- [Bifrost shows the latest unbound reaction and preserves remaining reactors when one removes theirs](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L70)
- [full OpenWA reaction snapshots and LID aliases reconstruct every actor and remove missing reactions](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L78)
- [private message IDs remain scoped to their original bot, regardless of who reacted in WhatsApp](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L86)
- [split messages react on the attachment once and skip Telegram copies already cleared](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L93)
- [a member bot absent from the group falls back to Bifrost](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L101)
- [unavailable emoji clears a stale native reaction and uses one attributed fallback in the same topic](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L106)
- [a rate-limited fallback notice is not lost when the original reaction event retries](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L118)
- [ambiguous native updates safely retry without uncertain outgoing-send effects](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L125)
- [Telegram-owner reactions echoing from WhatsApp do not add duplicate bot reactions or loop back](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L134)
- [later repeats of a reaction already represented by the Telegram owner remain suppressed](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L144)
- [WhatsApp heart variations normalize to Telegram emoji and do not create a second reaction](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L155)
- [unsupported Telegram custom reactions do not clear an existing WhatsApp reaction](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L162)
- [Bifrost maps laughing reactions on owner messages, deduplicates equivalent changes and clears on removal](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L168)
- [equivalent reactions work through member bots and private copies without altering supported emoji](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L178)
- [a refused equivalent and an unsupported unmapped emoji both keep the original in text fallbacks](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L186)
- [Telegram-owner reaction echoes stay suppressed after adding equivalent rendering](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L195)
- [Telegram custom reactions forward their Unicode equivalent and cache it without converting it to Telegram approximations](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L204)
- [member DM reactions are admitted with bot-scoped mappings while group duplicates are ignored](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L214)
- [adding a custom reaction beside an existing reaction forwards the new one; removal keeps the remaining one](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L223)
- [unresolvable custom reactions produce a temporary independent notice without clearing WhatsApp or blocking a chat](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L235)
- [a member unable to access another bot’s message falls back to Bifrost and cached refusals do not suppress that fallback](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L244)
- [old definitive reaction refusals expire so corrected permissions can recover](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L259)
- [custom-emoji resolution and outbound payload stay frozen across a known send retry](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/reactions.test.ts#L269)

## revocations.test.ts

- [a deleted owner message gets Delete/Keep without overwriting or deleting its Telegram content](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts#L62)
- [Keep removes the fallback notice and preserves the original, including after late edits/history](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts#L73)
- [own phone-originated messages get inline choices; Keep and expiration remove only the buttons](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts#L86)
- [Delete removes every text/media part in the chosen destination while preserving other copies and mappings](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts#L97)
- [other people’s deleted text/media copies are removed automatically without a replacement or choices](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts#L108)
- [duplicate events and conflicting clicks cannot reopen or reverse a completed choice](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts#L115)
- [choices validate owner, bot, destination, message, session and expiry before changing content](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts#L127)
- [expiration defaults to keeping the message and never deletes the original](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts#L140)
- [member-bot private copies keep their bot-scoped identity even when Telegram message IDs overlap](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts#L148)
- [a Telegram deletion refusal preserves the remaining copy and produces no destructive content replacement](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts#L158)
- [an ambiguous keyboard update safely retries without pausing as an uncertain send](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts#L168)
- [saved WhatsApp phone events identify the owner for messages predating the fromMe metadata](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts#L178)
- [fallback prompt expiration keeps the owner’s original message](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts#L186)
- [a timed-out automatic deletion retries safely without an uncertain message send](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts#L193)
- [a timed-out callback acknowledgement retries without blocking the conversation as uncertain](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/revocations.test.ts#L202)

## setup.test.ts

- [setup accepts OpenWA ready, migrates before local Telegram calls and installs both webhooks](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/setup.test.ts#L32)
- [setup refuses incomplete or disconnected sessions before migrating or registering callbacks](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/setup.test.ts#L42)

## statuses.test.ts

- [Status webhooks share one archive topic with linked authors and native times, without new conversations](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L60)
- [startup upgrades only bridge callback events, imports available stored Status posts and deduplicates webhook races](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L79)
- [a fresh or wildcard webhook installation remains idempotent](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L98)
- [image, video and voice Status posts use native media with attributed captions in the feed](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L108)
- [Status media uses the authenticated local status endpoint and never an advertised media URL](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L123)
- [omitted or expired media keeps its caption and a readable unavailable notice](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L132)
- [expired and malformed incoming stories are skipped while already archived posts remain](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L143)
- [typing, edits, message actions and reactions in the Status topic cannot publish or send WhatsApp messages](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L156)
- [/statuses opens and refreshes the feed from Bifrost DM; /status remains the health command](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L172)
- [replying /mute in Status suppresses only that author before media download, preserving chats and archived posts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L187)
- [refresh honors mutes and unmuting resumes new posts without replaying stories skipped while muted](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L206)
- [muted-person preferences survive reopening SQLite and resolve aliases with the latest preference winning](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L222)
- [muted list supports one-tap unmute and pagination without changing other people’s preferences](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L239)
- [mute controls enforce owner, bot, session, chat, topic and expiry; stale buttons cannot undo a later mute](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L260)
- [retrying a mute confirmation or unmute callback cannot reverse a newer decision from another chat](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L284)
- [Status mute commands require a mapped Status reply and ordinary conversation mute keeps its existing behavior](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L299)
- [Status topic clear log preserves posts, while clear all removes only this topic’s recorded posts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/statuses.test.ts#L311)

## stickers.test.ts


## sync.test.ts

- [sync updates group metadata, existing member bots and missing-bot buttons without WhatsApp writes](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts#L67)
- [an inbox group only renames its topic and never overwrites the shared management group](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts#L84)
- [content-hash picture caching avoids repeated uploads even when signed source URLs rotate](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts#L92)
- [hidden pictures and unavailable descriptions preserve Telegram data](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts#L108)
- [incomplete metadata and failed lookups cannot clear the old roster or group identity](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts#L117)
- [missing group permission yields a scoped grant button while contact metadata can still sync](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts#L129)
- [one contact bot shared across groups is fetched once and LID aliases retain its binding](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts#L143)
- [/sync scopes to its conversation, and global sync uses the local management queue](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts#L156)
- [/sync in a bound member bot DM works without a one-to-one route and reports back through that bot](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts#L172)
- [an unbound member bot explains the missing binding for Start and /sync without accessing WhatsApp](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts#L195)
- [member-bot DM sync keeps the contact scope with an existing inbox topic and rejects global sync](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts#L209)
- [a failed group lookup does not prevent existing contact bots from syncing](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts#L225)
- [chat jobs take priority over metadata work, and coalesced manual requests receive a completion](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts#L235)
- [cron is timezone-aware and persists one run per scheduled minute across restarts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts#L248)
- [cron supports quarter-hour, daily, weekday and disabled schedules and rejects malformed fields](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/sync.test.ts#L258)

## transfer.test.ts

- [import keeps credentials literal, normalizes only local paths and removes the retired n8n setting](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/transfer.test.ts#L9)
- [export discovers legacy and Bifrost named volumes and refuses ambiguous or bind-mounted state](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/transfer.test.ts#L18)
- [archive validation rejects traversal, duplicate paths and link entries before extraction](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/transfer.test.ts#L32)
- [export/import round trip preserves identity, state, media and ownership without starting the restored bots](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/transfer.test.ts#L53)

## unmirror.test.ts

- [/unmirror queues a durable move and restores the topic controls without deleting archived messages](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/unmirror.test.ts#L34)
- [new WhatsApp messages and Telegram replies use management while the former mirror stops sending](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/unmirror.test.ts#L61)
- [notification retries reuse the created topic and leave uncertain deliveries intact](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/unmirror.test.ts#L81)
- [topic permission errors keep the existing mirror and explain how to retry](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/unmirror.test.ts#L96)
- [uncertain topic creation is preserved for review without replaying the creation](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/unmirror.test.ts#L105)
- [muted and ignored mirrors retain their setting after returning to management](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/unmirror.test.ts#L113)
- [an active history import finishes before the mirror can be moved](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/unmirror.test.ts#L123)
- [stale move requests and notification retries cannot undo later routing choices](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/unmirror.test.ts#L132)
- [/unmirror requires the owner, master bot and a linked WhatsApp mirror group](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/unmirror.test.ts#L146)

## Additional inspected requirements

- **Managed bot registration and binding** — [registration and binding](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); bridge tests cover owner management, pending provisioning, alias reuse, encrypted tokens and capacity fallback. The explicit duplicate-contact binding rejection is source-inspected.
- **Poll choice controls** — [poll action handling](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/bridge.ts); control/bridge fixtures cover poll relays and owner-only command behavior. Multiple-choice toggle state is source-inspected, not independently verified against a real WhatsApp poll.

## Verification limits

- Provider permissions, managed-bot capabilities, real message/media availability, API server compatibility and SDK release coverage require integration verification during the port.
- Archive validation/round-trip Docker tests and codec fixtures are specified from source/docs and skipped fixtures; they were not exercised here.
- No numeric coverage guarantee is inferred from this inventory.

## Opt-in codec cases (not executed)

- [static WebP passes through byte-for-byte; non-square stickers get transparent padding](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/stickers.test.ts#L44)
- [animated WebP becomes a moving transparent VP9 sticker with variable frame timing](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/stickers.test.ts#L54)
- [long animations preserve their ending while speeding up to three seconds](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/stickers.test.ts#L69)
- [Telegram VP9 alpha survives conversion to WhatsApp animated WebP](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/stickers.test.ts#L76)
- [timeout kills encoder descendants and removes the private job directory](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/stickers.test.ts#L93)
- [TGS uses its frame rate and nonzero in-point, preserving motion and transparency](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/stickers.test.ts#L114)
- [dense WhatsApp animations fit after extra compression while retaining motion, duration and alpha](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/stickers.test.ts#L136)
- [malformed and excessive inputs fail safely; concurrent jobs leave no intermediates](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/stickers.test.ts#L167)
- [high-detail animations either meet the video size ceiling or retain the original fallback](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/stickers.test.ts#L177)
