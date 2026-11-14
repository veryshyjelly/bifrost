# Runtime storage and interfaces

## Durable PostgreSQL state

| Area | Authoritative state |
| --- | --- |
| Identity | Permanent account ID and encryption-key fingerprint |
| Jobs | Deduplication, stream order, claim tokens, leases, retries and terminal states |
| Effects | Frozen requests, pending/sending/done/uncertain state and immutable receipts |
| Routing | Conversations, generations, mirrors, topic ownership and retirement reservations |
| Copies and sources | Scoped mappings, source edits/tombstones, observations and completion evidence |
| Credentials | Encrypted bot tokens, credential revisions, hosted-logout proof and native local identity checks |
| Workflows | Frozen history batches, metadata stages/cron, Status mutes/skips and control plans |
| Retained evidence | Read-only compatibility records, activation barriers and stable scoped keys |

Applied migrations remain unchanged. No archive importer or projector is part of
the current CLI. Retained-state readers do not fabricate receipts, reopen archived
work or make incomplete restores runnable.

## Assets and configuration

`BRIDGE_MEDIA` contains hash-addressed original and converted bytes under
`bifrost-go-assets`. Frozen plans reference those bytes across retries. Persistent
sticker lookups add small source/target/policy records; temporary-work cleanup
never removes frozen assets or successful conversion lookups.

The runtime reads literal environment variables. Keep `BIFROST_ACCOUNT_ID`,
encryption keys and original credentials with private backups. Diagnostic `serve`
never dispatches; explicit `run` owns the provider bootstrap, worker and scheduler.
See [configuration and runtime](../docs/RUNTIME.md) and [phone setup](../docs/PHONE-HOST.md).

## HTTP boundary

Liveness uses `/healthz`. `/readyz` reports 503 during setup/shutdown and 200 only
when native identities, callbacks and runtime configuration are ready. Management
and recovery actions require their configured authentication and explicit scope.
Ingress admissions are authenticated and durable, with duplicate suppression.
`BRIDGE_BIND_HOST` is an optional IP literal; Compose publishes ports on loopback,
and the native phone sets the bridge listener to `127.0.0.1`.

Keep response and command presentation stable. Unknown native sends remain
uncertain for review; generic job skipping does not reconcile topic guards.
