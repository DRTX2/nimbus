#!/usr/bin/env bash
# Restore drill: dumps a Nimbus database, restores it into a disposable
# PostgreSQL with the runtime roles provisioned first, then proves the copy is
# servable (migration checksums match this binary) and complete (exact row
# counts per table). Prints the elapsed time as evidence for recovery targets.
#
# Usage: NIMBUS_RESTORE_SOURCE_URL=postgres://... scripts/restore-drill.sh
# The source role must bypass row-level security (superuser or BYPASSRLS):
# FORCE RLS hides tenant rows from every other role, including the owner.
set -euo pipefail

SOURCE=${NIMBUS_RESTORE_SOURCE_URL:?set NIMBUS_RESTORE_SOURCE_URL to a superuser or BYPASSRLS connection URL}
NAME=${NIMBUS_RESTORE_CONTAINER:-nimbus-restore-drill}
PORT=${NIMBUS_RESTORE_PORT:-55433}
IMAGE=postgres:17.5-alpine@sha256:6567bca8d7bc8c82c5922425a0baee57be8402df92bae5eacad5f01ae9544daa
TARGET="postgres://nimbus:nimbus@127.0.0.1:${PORT}/nimbus_restore?sslmode=disable"
WORK=$(mktemp -d)
trap 'docker rm -f "$NAME" >/dev/null 2>&1 || true; rm -rf "$WORK"' EXIT

pg() { docker run --rm -i --network host -v "$WORK:/work" "$IMAGE" "$@"; }

row_counts() {
  pg psql -X -At -v ON_ERROR_STOP=1 "$1" <<'SQL'
SELECT format('SELECT %L || '' '' || count(*) FROM public.%I', table_name, table_name)
  FROM information_schema.tables
 WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
 ORDER BY table_name
\gexec
SQL
}

started=$(date +%s)
pg pg_dump --format=custom --file=/work/nimbus.dump "$SOURCE"
row_counts "$SOURCE" >"$WORK/source.counts"

docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d --rm --name "$NAME" -e POSTGRES_DB=nimbus_restore -e POSTGRES_USER=nimbus \
  -e POSTGRES_PASSWORD=nimbus -p "127.0.0.1:${PORT}:5432" "$IMAGE" >/dev/null
for _ in $(seq 1 60); do
  docker exec "$NAME" pg_isready -U nimbus -d nimbus_restore >/dev/null 2>&1 && break
  sleep 0.5
done
sleep 1
# Roles are cluster-wide and absent from a database dump; grants, ownership
# and RLS policies in the dump reference them, so they must exist first.
docker exec -i "$NAME" psql -q -U nimbus -d nimbus_restore -v ON_ERROR_STOP=1 <<'SQL'
CREATE ROLE nimbus_owner NOLOGIN;
CREATE ROLE nimbus_api NOLOGIN;
CREATE ROLE nimbus_worker NOLOGIN;
SQL

pg pg_restore --exit-on-error --dbname="$TARGET" /work/nimbus.dump
NIMBUS_DATABASE_URL="$TARGET" go run ./cmd/nimbus verify-schema
row_counts "$TARGET" >"$WORK/target.counts"

if ! diff -u "$WORK/source.counts" "$WORK/target.counts"; then
  echo "restore drill FAILED: row counts differ" >&2
  exit 1
fi
echo "restore drill passed: $(wc -l <"$WORK/source.counts") tables, $(awk '{s+=$2} END {print s}' "$WORK/source.counts") rows, $(($(date +%s) - started))s"
