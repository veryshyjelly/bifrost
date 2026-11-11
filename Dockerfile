FROM golang:1.27.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download github.com/jackc/pgx/v5 github.com/pressly/goose/v3
COPY cmd/bifrost ./cmd/bifrost
COPY internal ./internal
COPY migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/bifrost ./cmd/bifrost

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates ffmpeg chromium python3 python3-venv \
    && python3 -m venv /opt/sticker-codec \
    && /opt/sticker-codec/bin/pip install --no-cache-dir Pillow==13.0.0 \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --create-home bifrost \
    && mkdir -p /media /data && chown bifrost:bifrost /media /data
ENV STICKER_PYTHON=/opt/sticker-codec/bin/python STICKER_CHROMIUM=/usr/bin/chromium
COPY --from=build /out/bifrost /usr/local/bin/bifrost
USER bifrost
WORKDIR /app
ENTRYPOINT ["bifrost"]
CMD ["serve"]
