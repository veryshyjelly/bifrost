# Explicit topic reconciliation

Diagnostic and future runtime HTTP servers expose authenticated `GET /v1/topic-review?job=<id>` and `POST /v1/topic-review`. The CLI uses those endpoints and does not open a database, reset identity or contact Telegram. Keep `BRIDGE_ADMIN_TOKEN` in the environment. `BIFROST_ACCOUNT_ID`, when set, also protects the CLI against reviewing a different server account.

Inspect an interrupted initial inbox, mirror return, native repair or Status setup with:

```sh
bifrost inspect-topic --job 123
```

The result contains the permanent account, current job/effect states, frozen management chat/name, request digest and scope. A `blocked` reason means the job cannot currently be reconciled. Live jobs, missing/changed guards, route generations, active history, already delivered source parts and unrelated uncertain effects remain protected. Inspection uses a read-only consistent database snapshot.

Prepare a private JSON decision from that inspection. Keep its `operationId` stable when retrying the same decision:

```json
{
  "operationId": "review-123-1",
  "accountId": "copy-the-inspected-account",
  "job": 123,
  "effect": "copy-the-inspected-effect",
  "expectedState": "uncertain",
  "expectedEffectState": "uncertain",
  "requestDigest": "copy-the-inspected-64-character-digest",
  "action": "adopt",
  "management": -1001234567890,
  "threadId": 456,
  "confirmCreated": true
}
```

Replace every example value with the inspected state and exact created topic link, then run:

```sh
bifrost reconcile-topic --file reviewed-decision.json
```

The pinned Telegram API/SDK has no method for reading an arbitrary forum topic. Adoption therefore relies on the operator’s explicit confirmation; it does not claim remote existence verification. It records a manual receipt for that exact topic under the original effect key/request and resumes the original job. The route or feed changes only through its normal fenced commit. Its guard remains in place until then, and no replacement topic is created.

If inspection and checking Telegram establish that no topic was created, use `action: "absent"`, omit `threadId`/`confirmCreated`, and set `confirmNoTopic: true`. This resets only the inspected unresolved topic effect. A frozen guard whose effect was never prepared is reported as `unprepared`; the same explicit decision can resume its original request. Completed receipts cannot be erased or replaced. Adopting an already confirmed exact topic leaves its stored request and receipt unchanged.

The transaction validates all evidence again and rejects stale states, changed requests, wrong accounts/management chats, live row locks, retired or occupied topics, old missing-topic IDs and unrelated uncertainty. It commits the receipt/reset, resumed job and immutable decision audit together. A failed audit insert rolls everything back. Reusing an operation ID with different input fails; exact replay returns the recorded result without changing subsequent runtime work or later owner route choices.

Generic recovery remains available:

```sh
bifrost review --job 123 --action retry --confirm-no-delivery
bifrost review --job 123 --action skip
```

Generic skip leaves unresolved topic ownership and guards intact. Topic reconciliation can explicitly resume such a skipped job. Neither command reconciles uncertain photo uploads or hosted logout by inferring success. HTTP/CLI errors omit credentials and provider descriptions; CLI requests reject redirects. `--url` can select an explicit bridge API base URL, otherwise the CLI uses localhost and `BRIDGE_PORT` (default `3210`). No runtime is started by these commands.

Status setup also checks its frozen request against the management target; configuration changes cannot retarget unfinished setup. Prepared Status deliveries reuse their original archive destination rather than creating a replacement feed during retry.

Native PostgreSQL fixtures exercise all four scopes, guard retention after skip, explicit absence, unprepared effects, confirmed receipt immutability, reservations/occupied topics, stale evidence, live locks, unrelated uncertainty, idempotent replay and audit rollback. HTTP and loopback CLI fixtures cover authentication, strict input, account scope and redirect refusal. Final existing import/runtime composition remain separate completion work.
