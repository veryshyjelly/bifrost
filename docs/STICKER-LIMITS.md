# Sticker limits and conversion behavior

## WhatsApp budget

The encoder budget is **1,000,000 bytes for the complete static or animated WebP,
including metadata**. Compatibility measurements found a usable boundary of
1,048,558 bytes and refusal at 1,048,560 bytes. Keep the lower encoder budget;
do not target the boundary before adding metadata.

Size is only one constraint. Validate the RIFF length, chunk structure, dimensions,
decoding and animation timing. A provider can acknowledge an invalid container
without the recipient rendering it. Padding outside the declared RIFF container
and unsupported simple-container layouts do not establish usable size limits.
Preserve Telegram's separate sticker and media limits.

## Animation

Use decoded frames and presentation timestamps when container duration metadata
is inconsistent. A WebM can contain multiple seconds of animation while reporting
a duration of only a few milliseconds; trusting that field can produce a single
frame. Preserve frame timing, transparency and the existing document fallback.
Check recipient rendering separately from API acknowledgement.

## Persistent conversion cache

Successful sticker conversions have a persistent lookup in the existing
`bifrost-go-assets` directory. The key includes the verified source content hash
and size, destination, codec policy revision, embedded helper/renderer contents
and WhatsApp byte budget. Names and provider file IDs do not fragment the cache.
Static WebP, animated WebP and Telegram video results retain their exact bytes
and format. Both source and output hashes are checked on reuse; cache hits run
no renderer, decoder or transcoder subprocesses.

The first conversion still takes its normal time. Entries warm under the fixed
codec; older unversioned conversions are not adopted. Concurrent requests share
a conversion. A cancelled waiter does not cancel a separate caller's work.
Failed or cancelled conversions are not cached. Invalid, missing or damaged
entries are misses; immutable asset corruption remains subject to asset
verification and fallback rules. Cache-write failures do not discard a valid
conversion. Frozen plans, source bytes, native sends and original-document
fallback choices keep their existing recovery behavior.

Lookup files use the `.sticker-cache-` prefix, are published atomically and
survive temporary-work cleanup and process restarts. Converted blobs belong to
the retained asset store; the cache adds only small lookup records. Bump
`stickerCacheRevision` whenever conversion, timing or validation policy changes.
Helper/renderer changes invalidate the key automatically.
