#!/bin/sh
# The official PostgreSQL entrypoint runs this only for a fresh data directory.
# Keep application owners separate. Existing volumes retain their roles and
# databases; changing this script is not a migration of an installed instance.
set -eu
: "${BIFROST_DB_PASSWORD:?Set the Bifrost database password}"
: "${OPENWA_DB_PASSWORD:?Set the OpenWA database password}"
psql --username "$POSTGRES_USER" --dbname postgres --set=ON_ERROR_STOP=1 \
  --set=bifrost_password="$BIFROST_DB_PASSWORD" \
  --set=openwa_password="$OPENWA_DB_PASSWORD" <<'SQL'
CREATE ROLE bifrost LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'bifrost_password';
CREATE ROLE openwa LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'openwa_password';
CREATE DATABASE bifrost OWNER bifrost;
CREATE DATABASE openwa OWNER openwa;
REVOKE ALL ON DATABASE bifrost FROM PUBLIC;
REVOKE ALL ON DATABASE openwa FROM PUBLIC;
GRANT CONNECT, TEMPORARY ON DATABASE bifrost TO bifrost;
GRANT CONNECT, TEMPORARY ON DATABASE openwa TO openwa;
SQL
