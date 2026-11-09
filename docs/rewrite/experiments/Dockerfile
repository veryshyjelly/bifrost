# Offline verification image only. No bridge entry point or production volumes.
FROM oven/bun:1.4.2-debian@sha256:4f6e31d1a54d6a3dd312daef655fc998101b5043d52e12592ac293ef04b9bc73
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production
COPY . .
USER bun
CMD ["bun", "test", "./test/rewrite"]
