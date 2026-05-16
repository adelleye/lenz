#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

POSTGRES_PORT="${POSTGRES_PORT:-55432}"
API_PORT="${API_PORT:-3001}"
GO_BIN="${GO_BIN:-go}"
DATABASE_URL="${DATABASE_URL:-postgres://lenzcore:lenzcore123@localhost:${POSTGRES_PORT}/lenzcore?sslmode=disable}"
BASE_URL="http://localhost:${API_PORT}"
ACCOUNT_ID="44444444-4444-4444-4444-444444444444"
CUSTOMER_ID="33333333-3333-3333-3333-333333333333"

API_PID=""
TMP_DIR=""

fail() {
  echo "FAIL: $*" >&2
  if [[ -n "${API_LOG:-}" && -f "$API_LOG" ]]; then
    echo "--- API log ---" >&2
    tail -80 "$API_LOG" >&2 || true
  fi
  exit 1
}

pass() {
  echo "PASS: $*"
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

compose() {
  POSTGRES_PORT="$POSTGRES_PORT" docker compose -f infra/docker/docker-compose.yml "$@"
}

cleanup() {
  if [[ -n "$API_PID" ]]; then
    kill "$API_PID" >/dev/null 2>&1 || true
    wait "$API_PID" >/dev/null 2>&1 || true
  fi
  if [[ -n "$TMP_DIR" && -d "$TMP_DIR" ]]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT

json_get() {
  jq -r "$1" <<<"$2"
}

assert_json() {
  local json="$1"
  local filter="$2"
  local message="$3"
  if ! jq -e "$filter" <<<"$json" >/dev/null; then
    jq . <<<"$json" >&2 || echo "$json" >&2
    fail "$message"
  fi
}

request() {
  curl -fsS "$@"
}

wait_container_healthy() {
  local name="$1"
  local status
  for _ in $(seq 1 60); do
    status="$(docker inspect -f '{{.State.Health.Status}}' "$name" 2>/dev/null || true)"
    if [[ "$status" == "healthy" ]]; then
      return 0
    fi
    sleep 2
  done
  fail "container did not become healthy: $name"
}

wait_api() {
  for _ in $(seq 1 60); do
    if request "${BASE_URL}/api/v1/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  fail "API did not become healthy on ${BASE_URL}"
}

assert_balance() {
  local want="$1"
  local body
  body="$(request "${BASE_URL}/api/v1/accounts/${ACCOUNT_ID}/balance")"
  assert_json "$body" ".available_minor == ${want} and .ledger_minor == ${want}" "expected account balance ${want}"
}

assert_journal_balanced() {
  local journal_id="$1"
  local amount="$2"
  local body
  body="$(request "${BASE_URL}/api/v1/admin/ledger/journal/${journal_id}")"
  assert_json "$body" ".balanced == true" "journal ${journal_id} is not marked balanced"
  assert_json "$body" "([.postings[] | select(.direction == \"debit\") | .amount_minor] | add) == ${amount}" "journal ${journal_id} debit amount mismatch"
  assert_json "$body" "([.postings[] | select(.direction == \"credit\") | .amount_minor] | add) == ${amount}" "journal ${journal_id} credit amount mismatch"
}

require_cmd docker
require_cmd curl
require_cmd jq
require_cmd "$GO_BIN"

if ! docker info >/dev/null 2>&1; then
  if command -v colima >/dev/null 2>&1; then
    echo "Docker engine is not running; starting Colima..."
    colima start --cpu 2 --memory 4 --disk 20
  else
    fail "Docker engine is not running. Start Docker Desktop or Colima, then rerun this script."
  fi
fi

if command -v lsof >/dev/null 2>&1 && lsof -tiTCP:"$API_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  fail "API port ${API_PORT} is already in use. Stop that process or set API_PORT to another port."
fi

echo "Resetting Docker Compose services and volumes for a clean demo database..."
compose down -v --remove-orphans >/dev/null
compose up -d postgres redis >/dev/null
wait_container_healthy lenzcore-postgres
wait_container_healthy lenzcore-redis
pass "Docker Compose started healthy Postgres and Redis"

echo "Running migrations..."
DATABASE_URL="$DATABASE_URL" "$GO_BIN" run ./apps/core/cmd/migrate >/tmp/lenz-core-demo-migrate.log
cat /tmp/lenz-core-demo-migrate.log
pass "migrations applied"

echo "Running unit tests..."
"$GO_BIN" test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
pass "unit test suite passed"

echo "Running Postgres-backed integration tests..."
LENZ_INTEGRATION_DATABASE_URL="$DATABASE_URL" "$GO_BIN" test -count=1 -tags=integration ./apps/core/internal/corebanking -run TestSQLStoreTransferSpineIntegration
pass "SQL integration test suite passed"

TMP_DIR="$(mktemp -d)"
API_LOG="${TMP_DIR}/api.log"
"$GO_BIN" build -o "${TMP_DIR}/lenz-core-api" ./apps/core
DATABASE_URL="$DATABASE_URL" PORT="$API_PORT" "${TMP_DIR}/lenz-core-api" >"$API_LOG" 2>&1 &
API_PID="$!"
wait_api
pass "API started successfully"

health="$(request "${BASE_URL}/api/v1/health")"
assert_json "$health" '.status == "ok"' "health endpoint did not return ok"
pass "GET /api/v1/health returned ok"

seed="$(request -X POST "${BASE_URL}/api/v1/demo/seed")"
assert_json "$seed" '.institution.id == "11111111-1111-1111-1111-111111111111" and .customer.id == "33333333-3333-3333-3333-333333333333" and .account.id == "44444444-4444-4444-4444-444444444444"' "demo seed response mismatch"
pass "POST /api/v1/demo/seed created demo tenant data"

accounts="$(request "${BASE_URL}/api/v1/customers/${CUSTOMER_ID}/accounts")"
assert_json "$accounts" 'length == 1 and .[0].id == "44444444-4444-4444-4444-444444444444" and .[0].account_number == "9990000001"' "customer accounts response mismatch"
pass "customer account lookup returned the demo account"

inbound="$(request -X POST "${BASE_URL}/api/v1/transfers/mock/inbound" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-script-in-001' \
  -d "{\"account_id\":\"${ACCOUNT_ID}\",\"amount_minor\":500000,\"provider_event_id\":\"demo-script-provider-event-001\",\"provider_reference\":\"demo-script-in-ref-001\",\"narration\":\"Demo script inbound\"}")"
assert_json "$inbound" '.status == "succeeded" and .amount_minor == 500000 and .journal_entry_id != null' "inbound transfer did not succeed with a journal"
inbound_id="$(json_get '.id' "$inbound")"
inbound_journal_id="$(json_get '.journal_entry_id' "$inbound")"
assert_balance 500000
assert_journal_balanced "$inbound_journal_id" 500000
pass "mock inbound credited the account and posted a balanced journal"

dup_idempotency="$(request -X POST "${BASE_URL}/api/v1/transfers/mock/inbound" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-script-in-001' \
  -d "{\"account_id\":\"${ACCOUNT_ID}\",\"amount_minor\":500000,\"provider_event_id\":\"demo-script-provider-event-001\",\"provider_reference\":\"demo-script-in-ref-001\",\"narration\":\"Demo script duplicate idempotency\"}")"
assert_json "$dup_idempotency" ".id == \"${inbound_id}\"" "duplicate idempotency key returned a different transfer"
assert_balance 500000
pass "duplicate idempotency key did not double-credit"

dup_provider="$(request -X POST "${BASE_URL}/api/v1/transfers/mock/inbound" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-script-in-002' \
  -d "{\"account_id\":\"${ACCOUNT_ID}\",\"amount_minor\":500000,\"provider_event_id\":\"demo-script-provider-event-001\",\"provider_reference\":\"demo-script-in-ref-001\",\"narration\":\"Demo script duplicate provider event\"}")"
assert_json "$dup_provider" ".id == \"${inbound_id}\"" "duplicate provider event returned a different transfer"
assert_balance 500000
pass "duplicate provider_event_id did not double-credit"

outbound="$(request -X POST "${BASE_URL}/api/v1/transfers/mock/outbound" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-script-out-001' \
  -d "{\"account_id\":\"${ACCOUNT_ID}\",\"amount_minor\":125000,\"provider_reference\":\"demo-script-out-ref-001\",\"narration\":\"Demo script outbound\"}")"
assert_json "$outbound" '.status == "succeeded" and .amount_minor == 125000 and .journal_entry_id != null' "outbound transfer did not succeed with a journal"
assert_balance 375000
pass "mock outbound debited the account"

failed="$(request -X POST "${BASE_URL}/api/v1/transfers/mock/outbound" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-script-failed-001' \
  -d "{\"account_id\":\"${ACCOUNT_ID}\",\"amount_minor\":999999999,\"narration\":\"Demo script insufficient funds\"}")"
assert_json "$failed" '.status == "failed" and .failure_reason == "insufficient_funds" and .journal_entry_id == null' "insufficient-funds transfer did not fail cleanly"
assert_balance 375000
failed_id="$(json_get '.id' "$failed")"
pass "insufficient-funds outbound failed without changing balance"

pending="$(request -X POST "${BASE_URL}/api/v1/transfers/mock/inbound" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-script-pending-001' \
  -d "{\"account_id\":\"${ACCOUNT_ID}\",\"amount_minor\":100000,\"provider_event_id\":\"demo-script-provider-event-pending-001\",\"status\":\"pending\",\"narration\":\"Demo script pending\"}")"
assert_json "$pending" '.status == "pending" and .journal_entry_id == null' "pending transfer posted money or did not remain pending"
assert_balance 375000
pending_id="$(json_get '.id' "$pending")"
pass "pending transfer recorded without posting money"

reversal="$(request -X POST "${BASE_URL}/api/v1/transfers/${inbound_id}/reverse" -H 'Idempotency-Key: demo-script-reversal-001')"
assert_json "$reversal" ".direction == \"reversal\" and .status == \"succeeded\" and .reversal_of_transfer_id == \"${inbound_id}\" and .journal_entry_id != null" "reversal did not create a succeeded reversal transfer"
reversal_id="$(json_get '.id' "$reversal")"
reversal_journal_id="$(json_get '.journal_entry_id' "$reversal")"
assert_journal_balanced "$reversal_journal_id" 500000
assert_balance -125000
pass "reversal created a new transfer and balanced journal entry"

transactions="$(request "${BASE_URL}/api/v1/accounts/${ACCOUNT_ID}/transactions")"
assert_json "$transactions" 'length == 5' "transaction history did not contain five Lenz transfer rows"
assert_json "$transactions" "[.[] | select(.transfer_id == \"${inbound_id}\" and .status == \"succeeded\" and .signed_minor == 500000 and .journal_entry_id != null)] | length == 1" "history missing posted inbound row"
assert_json "$transactions" "[.[] | select(.direction == \"outbound\" and .status == \"succeeded\" and .signed_minor == -125000 and .journal_entry_id != null)] | length == 1" "history missing posted outbound row"
assert_json "$transactions" "[.[] | select(.transfer_id == \"${pending_id}\" and .status == \"pending\" and .signed_minor == 0 and .journal_entry_id == null)] | length == 1" "history missing pending row"
assert_json "$transactions" "[.[] | select(.transfer_id == \"${failed_id}\" and .status == \"failed\" and .signed_minor == 0 and .journal_entry_id == null)] | length == 1" "history missing failed row"
assert_json "$transactions" "[.[] | select(.transfer_id == \"${reversal_id}\" and .direction == \"reversal\" and .signed_minor == -500000 and .journal_entry_id != null)] | length == 1" "history missing reversal row"
pass "transaction history came from Lenz transfer/journal/posting records"

transfers="$(request "${BASE_URL}/api/v1/admin/transfers")"
assert_json "$transfers" 'length == 5' "admin transfer list did not contain five transfer records"
pass "admin transfer list returned all demo transfers"

echo
echo "DEMO TRANSFER SPINE: PASS"
