# Capability and evidence index

The specifications are the behavior contract. The current implementation is Go;
source-level evidence is local to this checkout. Tests exercise native SDK wire
shapes and isolated PostgreSQL state. Fixtures establish the behavior exercised by their assertions; they do not
prove manual coverage of every provider workflow on every host.

| Capability | Runtime and persistence evidence |
| --- | --- |
| Identity and mention scope | [identity](../internal/identity), [mentions](../internal/mentions), [directory persistence](../internal/postgres/bots.go) |
| Routing and topics | [conversations](../internal/conversation), [controls](../internal/control), [route repair](../docs/ROUTE-REPAIR.md) |
| Text and media | [relay](../internal/relay), [presentation](../internal/presentation), [attachments](../internal/attachments) |
| Ingress and delivery | [ingress](../internal/ingress), [worker/effects](../internal/delivery), [PostgreSQL](../internal/postgres) |
| Managed bots and menus | [bots](../internal/bots), [menus](../internal/control/menu.go), [setup persistence](../internal/postgres/bots.go) |
| History and retained state | [history](../internal/history), [retained readers](../internal/postgres/retained.go), [stable keys](../internal/retained) |
| Metadata and scheduling | [metadata](../internal/metadata), [schedule](../internal/schedule), [run coordination](../docs/METADATA-RUNS.md) |
| Status | [statuses](../internal/statuses), [archive persistence](../internal/postgres/status_archive.go) |
| Edits, reactions, polls and deletion | [message changes](../docs/MESSAGE-CHANGES.md), [native reactions](../docs/NATIVE-REACTIONS.md), [polls](../docs/POLL-CONTROLS.md), [source actions](../internal/sourceactions) |
| Recovery and retirement | [topic reconciliation](../docs/TOPIC-RECONCILIATION.md), [inbox retirement](../docs/INBOX-RETIREMENT.md), [runtime ownership](../internal/postgres/runtime.go) |
| Composition | [runtime services](../cmd/bifrost/runtime_services.go), [native fixtures](../internal/postgres/native_test.go), [runtime fixtures](../internal/postgres/runtime_test.go) |

Run `make check` and `make spec-check`. Set `BIFROST_TEST_DATABASE_URL` only for an
isolated disposable PostgreSQL instance. Set `BIFROST_TEST_CODECS=1` with the tested
Python/ffmpeg/Chromium environment for real media tests. Historical implementation
snapshots remain in Git, rather than in the active documentation tree.
