# Provider adapter checks

The Go clients use `github.com/veryshyjelly/OpenWA/sdk/go` v0.11.1 and `github.com/go-telegram/bot` v1.28.0, versioned independently from the gateway. Both exact module versions resolve through the public Go module proxy without a local replacement. The gateway setup now uses the owner-maintained veryshyjelly/OpenWA fork at reviewed commit 6ef0ab6e9, based on OpenWA v0.30.0 with bounded-media and mention patches.

Run `make update-providers` to resolve the newest stable Go client modules and update go.mod/go.sum. Commit and validate the resulting exact pins. Gateway source pins are updated separately after reviewing the maintained fork and its upstream base; do not replace the fork pin with an upstream tag that omits its patches. Running the bridge never silently changes dependencies or upgrades a live gateway.

## Implemented coverage

- OpenWA: session readiness/restrictions, idempotent text sends with native quotes, passive stored-message lookup without inline media, and explicit-body edits including empty captions when the gateway supports them.
- Telegram: explicit getMe and native sendMessage parameters, including topics, replies and entities. Constructing a client does not poll, migrate a token or install webhooks.
- Source media: explicit SDK streaming download with cancellation, declared and streamed byte caps, original MIME/filename metadata, and distinct missing-source, disabled-download and unsupported-engine outcomes. This is an adapter operation; the media retrieval workflow is still pending.
- Transport: bounded JSON responses, no redirects, no SDK retry middleware or hidden retries on reused connections, safe error fields, rate delays and conservative mutation uncertainty. Raw SDK errors, provider bodies and tokens never become returned/logged error text.
- Media streaming, managed-bot operations, webhook admission, durable dispatch and conversation routing remain subsequent implementation slices. The clients alone do not constitute a working Go relay.

## Read-only diagnostics

The CLI reads provider URLs/credentials from its environment; it does not source private files implicitly:

```sh
bifrost check-openwa
bifrost check-telegram
```

The first command only checks its configured session. The second only calls getMe. Neither registers callbacks, restarts a session, changes read/view receipts or sends chat messages.

For an authorized test send, provide an explicit target, text and stable key:

```sh
bifrost check-openwa --chat "$TEST_CHAT" --text "$TEST_TEXT" --key "$STABLE_TEST_KEY"
```

Use `--session` to choose an authorized second session and `--quote` for a native quoted send. Reusing a key is appropriate only for the identical logical request. Never generate a new key to blindly replay an uncertain send. Use `--find-text` with `--chat` in a separate invocation to find that exact text in the latest stored message batch, without importing history or marking the chat read. CLI targets, message text and receipts are operational/private data; do not commit personal destinations or session details.

## Live evidence

The owner explicitly authorized the test group and their second phone/account. Three labeled logical sends succeeded: primary-session group text, primary-session DM and a second-session group reply quoting the primary test. The second session could passively observe the original group message. One completed request was repeated with the same key/body and returned the same message ID.

Receipts and stable keys are retained privately under ignored `data/provider-checks/`. No real test destinations, phone numbers or API keys are embedded in source fixtures or this document. These checks exercised the SDK/OpenWA/WhatsApp path through the existing live gateway, not end-to-end Go-to-Telegram conversation routing. The old bridge may still archive these test messages normally.

See [OpenWA migration](OPENWA-MIGRATION.md) before moving paired state or replacing the running gateway.

The owner-authorized [sticker size experiment](STICKER-LIMITS.md) measured the current WhatsApp Web upload boundary separately from receiver hashes and phone rendering. Properly structured static and animated WebPs worked at 1,048,558 bytes; 1,048,560 bytes was rejected by WhatsApp upload. This evidence does not change encoder policy or establish a limit for other provider APIs.
