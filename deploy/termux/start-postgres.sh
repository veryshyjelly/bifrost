#!/bin/sh
set -eu
umask 077
# Record the launcher identity before exec so graceful shutdown can identify the
# PostgreSQL process without signalling a later process that reused its PID.
mkdir -p /state/run
pid=$$
ticks=$(awk '{print $22}' /proc/$$/stat)
printf '{"pid":%s,"start_ticks":"%s"}\n' "$pid" "$ticks" > /state/run/postgres.tmp
mv /state/run/postgres.tmp /state/run/postgres.json
exec /usr/lib/postgresql/18/bin/postgres -D /state/postgres -h 127.0.0.1 -p 5432 -k /state/run
