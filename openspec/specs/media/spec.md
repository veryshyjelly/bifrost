# media

## Purpose

Relay attachments with bounded retrieval, native formats and safe original-file fallbacks.

The current Go runtime implements this contract; proposed behavior changes belong in change specs.

## Requirements

### Requirement: Native attachments

Supported images, video, voice, audio, documents and stickers SHALL use native Telegram methods, preserving captions, names, MIME metadata and reply mappings.

#### Scenario: View-once source

- **WHEN** OpenWA exposes bytes for view-once content
- **THEN** the available content is archived using the media relay path

### Requirement: Retrieval precedence

Historical attachments SHALL reuse retained Telegram media before stored OpenWA media; only a documented stored cache miss SHALL trigger bounded source download.

#### Scenario: Source route missing

- **WHEN** the download endpoint returns a route/session error
- **THEN** the bridge treats it as retryable configuration failure rather than declaring the source expired

### Requirement: Unavailable attachments

Unavailable or oversized source media SHALL retain its caption with a notice; temporary errors and rate limits SHALL retry rather than prove permanent absence.

#### Scenario: Source expired

- **WHEN** the provider definitively reports the attachment unavailable
- **THEN** a caption and unavailable notice complete that history receipt

### Requirement: Bounded downloads

Downloads SHALL enforce configured declared and streamed size limits, remove incomplete bytes on cancellation, and avoid read or Status-view receipts.

#### Scenario: Transfer cancelled

- **WHEN** a source transfer is aborted
- **THEN** partial bytes are removed and the operation remains retryable

### Requirement: Sticker conversion

WhatsApp stickers SHALL use Telegram-compatible static or animated formats; outgoing Telegram TGS/WebM stickers SHALL use WhatsApp WebP conversion. Failed conversion or definitive native rejection SHALL preserve the untouched original document fallback.

#### Scenario: Rate-limited sticker

- **WHEN** a prepared conversion receives 429
- **THEN** retry reuses its frozen conversion choice and bytes

#### Scenario: WebM duration disagrees with playback timestamps

- **WHEN** an outgoing video sticker's container claims a tiny duration but its video packets span a valid animation
- **THEN** conversion uses the complete bounded presentation timeline, including the final frame duration, and preserves motion in the WhatsApp WebP
- **AND** source bytes and previously frozen delivery plans remain unchanged

### Requirement: Temporary media cleanup

Temporary media files and abandoned sticker work directories SHALL be eligible for cleanup after 24 hours.

#### Scenario: Old work directory

- **WHEN** an abandoned sticker directory exceeds the retention age
- **THEN** periodic cleanup removes it

## Evidence

Source: [src/media.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/media.ts), [src/stickers.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/stickers.ts), [src/sticker-worker.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/src/sticker-worker.ts).

Regression fixtures: [media.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/media.test.ts), [stickers.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/stickers.test.ts), [bridge.test.ts](https://github.com/veryshyjelly/bifrost/blob/checkpoint-typescript-baseline/test/bridge.test.ts). See the [traceability index](../../traceability.md) for named cases and verification limits.
