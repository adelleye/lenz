#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

GO_BIN="${GO_BIN:-go}"
POSTGRES_PORT="${POSTGRES_PORT:-55432}"
DATABASE_URL="${DATABASE_URL:-postgres://lenzcore:lenzcore123@localhost:${POSTGRES_PORT}/lenzcore?sslmode=disable}"
SODA_ENV="${SODA_ENV:-development}"
SODA_PACKAGE="${SODA_PACKAGE:-github.com/gobuffalo/pop/v6/soda@v6.3.0}"

command_name="${1:-up}"
shift || true

case "$command_name" in
  up | down | status)
    ;;
  *)
    echo "usage: scripts/migrate.sh [up|down|status] [soda migrate flags]" >&2
    exit 2
    ;;
esac

DATABASE_URL="$DATABASE_URL" "$GO_BIN" run "$SODA_PACKAGE" \
  -c migrations/config/database.yml \
  -p migrations \
  -e "$SODA_ENV" \
  migrate "$command_name" "$@"
