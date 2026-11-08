FROM node:26.13.0-bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY package.json ./
COPY bridge.env.example ./
COPY src ./src
COPY scripts ./scripts
RUN mkdir -p /data /media && chown -R node:node /data /media
USER node
CMD ["node", "src/server.ts"]
