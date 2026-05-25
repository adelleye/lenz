#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

POSTGRES_PORT="${POSTGRES_PORT:-55432}"
GO_BIN="${GO_BIN:-go}"
DATABASE_URL="${DATABASE_URL:-postgres://lenzcore:lenzcore123@localhost:${POSTGRES_PORT}/lenzcore?sslmode=disable}"
RUN_MIGRATIONS="${RUN_MIGRATIONS:-true}"
START_DOCKER="${START_DOCKER:-true}"
DB_CONTAINER="${DB_CONTAINER:-lenzcore-postgres}"
POSTGRES_USER="${POSTGRES_USER:-lenzcore}"
POSTGRES_DB="${POSTGRES_DB:-lenzcore}"

INSTITUTION_ID="${INSTITUTION_ID:-11111111-1111-1111-1111-111111111111}"
INSTITUTION_NAME="${INSTITUTION_NAME:-Lenz CBA v0.1 Institution}"
INSTITUTION_SHORT_NAME="${INSTITUTION_SHORT_NAME:-Lenz CBA}"
INSTITUTION_CODE="${INSTITUTION_CODE:-999001}"
INSTITUTION_NUBAN_PREFIX="${INSTITUTION_NUBAN_PREFIX:-999}"

BRANCH_ID="${BRANCH_ID:-22222222-2222-2222-2222-222222222222}"
BRANCH_CODE="${BRANCH_CODE:-HQ}"
BRANCH_NAME="${BRANCH_NAME:-Head Office}"

SETTLEMENT_ACCOUNT_ID="${SETTLEMENT_ACCOUNT_ID:-55555555-5555-5555-5555-555555555555}"
SETTLEMENT_ACCOUNT_NUMBER="${SETTLEMENT_ACCOUNT_NUMBER:-9999999999}"
SETTLEMENT_ACCOUNT_NAME="${SETTLEMENT_ACCOUNT_NAME:-Internal Settlement Cash}"

LENZ_DEV_AUTH_TOKEN="${LENZ_DEV_AUTH_TOKEN:-local-dev-token}"

# shellcheck source=scripts/lib/cba_shell.sh
source "$ROOT_DIR/scripts/lib/cba_shell.sh"

run_psql() {
  if command -v psql >/dev/null 2>&1; then
    psql "$DATABASE_URL" \
      -v ON_ERROR_STOP=1 \
      -v institution_id="$INSTITUTION_ID" \
      -v institution_name="$INSTITUTION_NAME" \
      -v institution_short_name="$INSTITUTION_SHORT_NAME" \
      -v institution_code="$INSTITUTION_CODE" \
      -v institution_nuban_prefix="$INSTITUTION_NUBAN_PREFIX" \
      -v branch_id="$BRANCH_ID" \
      -v branch_code="$BRANCH_CODE" \
      -v branch_name="$BRANCH_NAME" \
      -v settlement_account_id="$SETTLEMENT_ACCOUNT_ID" \
      -v settlement_account_number="$SETTLEMENT_ACCOUNT_NUMBER" \
      -v settlement_account_name="$SETTLEMENT_ACCOUNT_NAME" \
      "$@"
    return
  fi

  require_cmd docker
  docker inspect "$DB_CONTAINER" >/dev/null 2>&1 || fail "missing psql and Docker container not found: $DB_CONTAINER"
  docker exec -i "$DB_CONTAINER" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
    -v ON_ERROR_STOP=1 \
    -v institution_id="$INSTITUTION_ID" \
    -v institution_name="$INSTITUTION_NAME" \
    -v institution_short_name="$INSTITUTION_SHORT_NAME" \
    -v institution_code="$INSTITUTION_CODE" \
    -v institution_nuban_prefix="$INSTITUTION_NUBAN_PREFIX" \
    -v branch_id="$BRANCH_ID" \
    -v branch_code="$BRANCH_CODE" \
    -v branch_name="$BRANCH_NAME" \
    -v settlement_account_id="$SETTLEMENT_ACCOUNT_ID" \
    -v settlement_account_number="$SETTLEMENT_ACCOUNT_NUMBER" \
    -v settlement_account_name="$SETTLEMENT_ACCOUNT_NAME" \
    "$@"
}

if [[ "$START_DOCKER" == "true" ]]; then
  ensure_docker_running
  compose up -d postgres >/dev/null
  wait_container_healthy "$DB_CONTAINER"
fi

if [[ "$RUN_MIGRATIONS" == "true" ]]; then
  require_cmd "$GO_BIN"
  DATABASE_URL="$DATABASE_URL" "$GO_BIN" run ./apps/core/cmd/migrate
fi

run_psql <<'SQL'
BEGIN;

INSERT INTO currencies (id, name, created_at, updated_at)
VALUES ('NGN', 'Nigerian Naira', now(), now())
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name,
    updated_at = EXCLUDED.updated_at;

INSERT INTO countries (id, name, flag, currency, is_supported, meta, created_at, updated_at)
VALUES ('NG', 'Nigeria', 'NG', 'NGN', true, '{}'::jsonb, now(), now())
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name,
    currency = EXCLUDED.currency,
    is_supported = EXCLUDED.is_supported,
    updated_at = EXCLUDED.updated_at;

INSERT INTO institutions (
    id,
    name,
    short_name,
    code,
    nuban_prefix,
    country_id,
    currency_id,
    status,
    meta,
    created_at,
    updated_at
)
VALUES (
    :'institution_id'::uuid,
    :'institution_name',
    :'institution_short_name',
    :'institution_code',
    :'institution_nuban_prefix',
    'NG',
    'NGN',
    'active',
    '{}'::jsonb,
    now(),
    now()
)
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name,
    short_name = EXCLUDED.short_name,
    code = EXCLUDED.code,
    nuban_prefix = EXCLUDED.nuban_prefix,
    country_id = EXCLUDED.country_id,
    currency_id = EXCLUDED.currency_id,
    status = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at;

INSERT INTO branches (
    id,
    institution_id,
    code,
    name,
    meta,
    status,
    created_at,
    updated_at
)
VALUES (
    :'branch_id'::uuid,
    :'institution_id'::uuid,
    :'branch_code',
    :'branch_name',
    '{}'::jsonb,
    'active',
    now(),
    now()
)
ON CONFLICT (id) DO UPDATE
SET code = EXCLUDED.code,
    name = EXCLUDED.name,
    status = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at;

INSERT INTO accounts (
    id,
    institution_id,
    customer_id,
    account_number,
    name,
    kind,
    product_type,
    allow_negative_balance,
    currency_id,
    normal_balance,
    status,
    created_at,
    updated_at
)
VALUES (
    :'settlement_account_id'::uuid,
    :'institution_id'::uuid,
    NULL,
    :'settlement_account_number',
    :'settlement_account_name',
    'internal',
    'internal',
    true,
    'NGN',
    'debit',
    'active',
    now(),
    now()
)
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name,
    kind = EXCLUDED.kind,
    product_type = EXCLUDED.product_type,
    allow_negative_balance = EXCLUDED.allow_negative_balance,
    currency_id = EXCLUDED.currency_id,
    normal_balance = EXCLUDED.normal_balance,
    status = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at;

INSERT INTO account_balances (
    account_id,
    institution_id,
    available_minor,
    ledger_minor,
    currency_id,
    updated_at
)
VALUES (
    :'settlement_account_id'::uuid,
    :'institution_id'::uuid,
    0,
    0,
    'NGN',
    now()
)
ON CONFLICT (account_id) DO NOTHING;

COMMIT;

SELECT id, name, short_name, code, status
FROM institutions
WHERE id = :'institution_id'::uuid;

SELECT id, institution_id, code, name, status
FROM branches
WHERE id = :'branch_id'::uuid;

SELECT id, institution_id, account_number, name, kind, product_type, allow_negative_balance, currency_id, normal_balance, status
FROM accounts
WHERE id = :'settlement_account_id'::uuid;
SQL

cat <<EOF

Bootstrap complete.

Use these env vars for local API testing:
export DATABASE_URL='${DATABASE_URL}'
export LENZ_DEV_AUTH_TOKEN='${LENZ_DEV_AUTH_TOKEN}'
export LENZ_DEV_INSTITUTION_ID='${INSTITUTION_ID}'
export APP_ENV='development'
export LENZ_DEMO_MODE='false'

Default branch_id for customer creation:
${BRANCH_ID}

Default internal settlement account:
${SETTLEMENT_ACCOUNT_ID}
EOF
