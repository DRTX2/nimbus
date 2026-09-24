#!/usr/bin/env bash
# Starts (or recreates) a disposable PostgreSQL for integration and acceptance
# tests with the same owner/api/worker role split used in production.
# Usage: scripts/test-db.sh [up|down|env]
set -euo pipefail

NAME=${NIMBUS_TEST_DB_CONTAINER:-nimbus-test-pg}
PORT=${NIMBUS_TEST_DB_PORT:-55432}
IMAGE=postgres:17.5-alpine@sha256:6567bca8d7bc8c82c5922425a0baee57be8402df92bae5eacad5f01ae9544daa

print_env() {
  cat <<ENV
export NIMBUS_TEST_DATABASE_URL='postgres://nimbus_owner:nimbus-owner-test@127.0.0.1:${PORT}/nimbus_test?sslmode=disable'
export NIMBUS_TEST_API_DATABASE_URL='postgres://nimbus_api:nimbus-api-test@127.0.0.1:${PORT}/nimbus_test?sslmode=disable'
export NIMBUS_TEST_WORKER_DATABASE_URL='postgres://nimbus_worker:nimbus-worker-test@127.0.0.1:${PORT}/nimbus_test?sslmode=disable'
ENV
}

case "${1:-up}" in
  up)
    docker rm -f "$NAME" >/dev/null 2>&1 || true
    docker run -d --rm --name "$NAME" -e POSTGRES_DB=nimbus_test -e POSTGRES_USER=nimbus \
      -e POSTGRES_PASSWORD=nimbus -p "127.0.0.1:${PORT}:5432" "$IMAGE" >/dev/null
    for _ in $(seq 1 60); do
      docker exec "$NAME" pg_isready -U nimbus -d nimbus_test >/dev/null 2>&1 && break
      sleep 0.5
    done
    sleep 1
    docker exec -i "$NAME" psql -q -U nimbus -d nimbus_test -v ON_ERROR_STOP=1 <<'SQL'
CREATE ROLE nimbus_owner LOGIN PASSWORD 'nimbus-owner-test';
CREATE ROLE nimbus_api LOGIN PASSWORD 'nimbus-api-test';
CREATE ROLE nimbus_worker LOGIN PASSWORD 'nimbus-worker-test';
ALTER DATABASE nimbus_test OWNER TO nimbus_owner;
ALTER SCHEMA public OWNER TO nimbus_owner;
SQL
    print_env
    ;;
  down) docker rm -f "$NAME" >/dev/null 2>&1 || true ;;
  env) print_env ;;
  *) echo "usage: $0 [up|down|env]" >&2; exit 2 ;;
esac
