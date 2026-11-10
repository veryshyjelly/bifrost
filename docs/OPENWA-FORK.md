# Maintained OpenWA fork

Gateway repository: https://github.com/veryshyjelly/OpenWA. Bifrost's default remote build uses the full reviewed commit `6ef0ab6e9d82751c62aefe93767ec1497ccefe6a`, verified against published main and the sibling OpenWa checkout. Its base is upstream OpenWA 0.30.0; the unmodified upstream release does not contain these patches. `OPENWA_SOURCE=../OpenWa` remains an explicit local-development override.

## Reviewed patch series

| Commit | Contract Bifrost depends on |
| --- | --- |
| 5d4575d1c | Explicit bounded source-media download after a stored-media miss, without read/view receipts or automatic archiving |
| 9a38202e6 | An ERROR resolver stage alone does not prove source absence; inspect the actual source result |
| ff24ec1b4 | With source path and media key available, bypass pending UI resolution while preserving retryable re-upload handling |
| a99b7fe2d | Normalize serialized WhatsApp mention identities into string IDs in new message metadata |

## Media retrieval contract

Use retained Telegram media first, then OpenWA stored media. Only a documented `MEDIA_NOT_STORED` miss permits the explicit source read:

`GET /api/sessions/:sessionId/messages/:chatId/:messageId/media/download?maxBytes=<positive integer>`

The source route supports whatsapp-web.js and returns 501 for unsupported engines. It validates the exact serialized message/chat identity and refuses status/broadcast or revoked content. The requested cap can only tighten the server cap. Lookup, queued work and download share a bounded timeout; cancelled/late page downloads retain their concurrency slot until the underlying operation settles.

Interpret outcomes explicitly: 404 `MEDIA_SOURCE_UNAVAILABLE` is source unavailability, not necessarily expiry; 413 is a size limit; 503/429 and transient source errors remain retryable. Missing routes/sessions and authorization refusals do not prove that media expired. Disabled downloads remain an explicit configuration limitation. Preserve `X-Media-Mimetype`, RFC 5987 filename metadata, cancellation and both declared/streamed byte limits. The endpoint does not mark read/viewed, write message rows or archive downloaded bytes.

Mention normalization improves new events; historical saved metadata still needs the existing string/serialized-object and alias handling in the Go mention port.

## Maintained Go SDK

The fork Go module is `github.com/veryshyjelly/OpenWA/sdk/go`. Version [v0.11.1](https://github.com/veryshyjelly/OpenWA/releases/tag/sdk/go/v0.11.1) is published at tag `sdk/go/v0.11.1`, commit `6ef0ab6e9`. Other language clients remain at their upstream v0.11.0 base and their missing source-media helper is documented explicitly in the fork coverage matrix.

`Messages.DownloadMedia(ctx, sessionID, chatID, messageID, maxBytes)` returns an owned `io.ReadCloser`, original MIME, content type, filename and declared size. Close every response, discard partial output on any read error, and treat filenames as untrusted metadata. The SDK reuses its authenticated HTTP client and timeout policy; it enforces both declared and streamed byte caps, preserves typed API errors and cancellation, and does not mark messages read or import history.

Bifrost's `SourceMedia` adapter uses that SDK method and its existing safe transport. Successful binary streams bypass the 8 MiB JSON buffer. Failed requests retain bounded error parsing, safe classifications and retry delays. Distinguish an unsupported engine or disabled download from retryable source conditions and never treat every 404 as expired media. This provides the operation needed by the later retrieval workflow; it does not enable automatic source downloads.

Standalone builds and Compose resolve the published SDK through Go modules, with no sibling checkout or local replacement. For coordinated development, a temporary workspace may link Bifrost and `../OpenWa/sdk/go`; do not commit machine-specific workspace files.

Updating the upstream base must retain this patch series or demonstrate equivalent upstream coverage. Gateway and Go SDK versions remain independent. No live deployment or paired state is changed by updating the build pin.
