FROM golang:1.27.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download github.com/jackc/pgx/v5 github.com/pressly/goose/v3
COPY cmd/bifrost ./cmd/bifrost
COPY internal ./internal
COPY migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/bifrost ./cmd/bifrost

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/* && useradd --uid 10001 --create-home bifrost && mkdir -p /media /data && chown bifrost:bifrost /media /data
COPY --from=build /out/bifrost /usr/local/bin/bifrost
USER bifrost
WORKDIR /app
ENTRYPOINT ["bifrost"]
CMD ["serve"]
