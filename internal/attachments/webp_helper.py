"""Local WebP decoding/encoding. Pillow uses libwebp's composited animation decoder."""
import json
import math
import sys
from io import BytesIO
from pathlib import Path

from PIL import Image

Image.MAX_IMAGE_PIXELS = 2048 * 2048


def alpha_canvas(data):
    # libwebp can optimize frames to opaque subrectangles and omit ALPHA_FLAG,
    # although the animation canvas is transparent. Request RGBA decoding and
    # retain that flag on encoded animations so other consumers see canvas alpha.
    data = bytearray(data)
    if data[:4] == b"RIFF" and data[8:16] == b"WEBPVP8X" and len(data) >= 30 and data[20] & 2:
        data[20] |= 16
    return bytes(data)


def inspect(path, directory=None):
    original = Path(path).read_bytes()
    normalized = alpha_canvas(original)
    with Image.open(BytesIO(normalized)) as image:
        if image.format != "WEBP" or max(image.size) > 2048:
            raise ValueError("Invalid sticker image")
        count = image.n_frames
        if count > 1200:
            raise ValueError("Too many frames")
        durations = []
        for i in range(count):
            image.seek(i)
            frame = image.convert("RGBA")  # load first: libwebp then exposes duration
            duration = image.info.get("duration", 100) if count > 1 else 100
            if duration <= 0:
                raise ValueError("Invalid frame duration")
            durations.append(duration)
            if sum(durations) > 60000:
                raise ValueError("Animation too long")
            if directory:
                frame.save(Path(directory) / f"frame{i:04d}.png")
        return dict(width=image.width, height=image.height, count=count,
                    durations=durations, duration=sum(durations) / 1000,
                    normalizeAlpha=normalized != original)


def encode(directory, output, quality, fps):
    directory = Path(directory)
    meta = json.loads((directory / "frames.json").read_text())
    duration = min(meta["duration"], 10)
    animated = meta["count"] > 1
    count = max(2, math.ceil(duration * fps)) if animated else 1
    frames, delays = [], []
    source_index, end = 0, meta["durations"][0]
    for i in range(count):
        timestamp = i * meta["duration"] * 1000 / count
        while timestamp >= end and source_index < meta["count"] - 1:
            source_index += 1
            end += meta["durations"][source_index]
        with Image.open(directory / f"frame{source_index:04d}.png") as image:
            frame = image.convert("RGBA")
            frame.thumbnail((512, 512), Image.Resampling.LANCZOS)
            canvas = Image.new("RGBA", (512, 512))
            canvas.paste(frame, ((512 - frame.width) // 2, (512 - frame.height) // 2))
            frames.append(canvas)
        delays.append(round((i + 1) * duration * 1000 / count) - round(i * duration * 1000 / count))
    frames[0].save(output, format="WEBP", save_all=animated, append_images=frames[1:],
                   duration=delays, loop=0, quality=quality, method=4)
    if animated:
        Path(output).write_bytes(alpha_canvas(Path(output).read_bytes()))


if __name__ == "__main__":
    operation, *args = sys.argv[1:]
    if operation == "inspect":
        print(json.dumps(inspect(*args)))
    elif operation == "encode":
        encode(args[0], args[1], int(args[2]), int(args[3]))
    else:
        raise ValueError("Unknown operation")
