> Historical TypeScript deployment documentation from `checkpoint-typescript-baseline`. Use this with the checkpoint checkout, not the unfinished Go branch.

# Sticker conversion

| Source | Delivered sticker |
| --- | --- |
| WhatsApp static WebP | Telegram static WebP |
| WhatsApp animated WebP | Telegram VP9 WebM, silent, at most 30 FPS / 3 seconds / 256,000 bytes |
| Telegram static WebP | WhatsApp static WebP, 512 square, at most 100,000 bytes |
| Telegram TGS or WebM | WhatsApp animated WebP, 512 square, at most 10 seconds / 500,000 bytes |

The bridge preserves aspect ratio with transparent padding. Longer animations are sped up to fit the destination duration, preserving the full sequence. Encoders reduce quality and frame rate to fit size limits, with additional compression attempts for dense WhatsApp animations before falling back to the original document. Ordinary documents are not converted into stickers. Source download limits still apply.

TGS uses a local Lottie light player through Puppeteer/Chromium, with autoplay and expressions disabled and external requests blocked. Frames go directly to WebP encoding without GIF or MP4 intermediates. Pillow decodes WebP frame blending/disposal and variable durations using libwebp. Animated WebP alpha flags are normalized so a transparent canvas remains transparent even when frames were optimized to opaque subrectangles. FFmpeg's libvpx decoder retains WebM alpha. Previously archived valid Telegram WebM stickers are reused byte-for-byte during history imports.

Jobs are serialized, use separate private directories and have a 120-second conversion deadline, including all encoder attempts. On failure or timeout the process group is killed and intermediates are removed. Compose enables an init process to reap orphaned encoder/browser processes. The normal media cleanup removes completed outputs and abandoned job directories after a day. Incoming labels, quotes and history mappings use the existing delivery receipts. Outgoing sticker retries retain the prepared file, conversion/fallback choice and content hash. If an expired file must be regenerated, its bytes and metadata must match before the original delivery key is reused.

Native limits follow [Telegram](https://core.telegram.org/stickers) and [WhatsApp](https://github.com/WhatsApp/stickers/blob/main/Android/README.md); byte ceilings are deliberately conservative. Inputs over 2048 pixels per dimension, 1200 WebP frames, 60 seconds or 4 MiB decompressed Lottie are refused by the converter and use the original-document fallback. The container runs Chromium as the `bun` user with its sandbox disabled for container compatibility; it handles only locally supplied Lottie with external requests blocked.

## Local validation

The default `bun run test` skips codec fixtures. CI runs them explicitly. Install FFmpeg with libvpx, Chromium/Chrome and Pillow **13.0.0** in a Python virtual environment, then:

```sh
bun install --frozen-lockfile
STICKER_PYTHON=/path/to/venv/bin/python \
STICKER_CHROMIUM=/path/to/chromium \
  bun run test:stickers
```

`STICKER_PYTHON` defaults to `python3`; `STICKER_CHROMIUM` defaults to `/usr/bin/chromium`. Both are set in the image. `STICKER_CHROMIUM_NO_SANDBOX=1` enables container-compatible launch arguments. These process environment settings are optional for local development; the bridge requires no new account configuration.

Tests generate their own sticker fixtures, check decoded pixels for transparency/motion/disposal, exercise timing and duration scaling, enforce size ceilings, and verify timeout/descendant cleanup. Bridge regressions cover native routing, original-document fallback, quotes, history and partial/rate-limited retries. These local checks do not send Telegram or WhatsApp messages.
