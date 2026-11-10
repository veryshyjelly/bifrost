# Build the official API server and its pinned TDLib submodule, rather than depending on an unversioned third-party image.
FROM debian:bookworm-slim AS build
ARG TELEGRAM_BOT_API_REF=7232881b39dfcd048a0d569f1983c5184249ee05
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates git g++ make cmake gperf libssl-dev zlib1g-dev && rm -rf /var/lib/apt/lists/*
WORKDIR /src
RUN git init && git remote add origin https://github.com/tdlib/telegram-bot-api.git && git fetch --depth 1 origin "$TELEGRAM_BOT_API_REF" && git checkout FETCH_HEAD && git submodule update --init --recursive --depth 1
RUN cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=/usr/local && cmake --build build --target install -j2
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libssl3 zlib1g && rm -rf /var/lib/apt/lists/* && useradd --uid 1000 --create-home telegram && mkdir /telegram-data && chown telegram:telegram /telegram-data
COPY --from=build /usr/local/bin/telegram-bot-api /usr/local/bin/telegram-bot-api
USER telegram
WORKDIR /telegram-data
ENTRYPOINT ["telegram-bot-api"]
CMD ["--local", "--http-port=8081", "--dir=/telegram-data", "--temp-dir=/telegram-data/tmp"]
