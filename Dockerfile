FROM oven/bun:1.4.2-debian@sha256:4f6e31d1a54d6a3dd312daef655fc998101b5043d52e12592ac293ef04b9bc73
USER root
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg ca-certificates chromium python3 python3-venv \
    && python3 -m venv /opt/sticker-python \
    && /opt/sticker-python/bin/pip install --no-cache-dir Pillow==13.0.0 \
    && rm -rf /var/lib/apt/lists/*
ENV STICKER_PYTHON=/opt/sticker-python/bin/python STICKER_CHROMIUM=/usr/bin/chromium STICKER_CHROMIUM_NO_SANDBOX=1
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production
COPY bridge.env.example ./
COPY src ./src
COPY scripts ./scripts
RUN mkdir -p /data /media && chown -R bun:bun /data /media
USER bun
CMD ["bun", "src/server.ts"]
