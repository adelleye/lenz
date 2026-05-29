#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

POSTGRES_PORT="${POSTGRES_PORT:-55432}"
API_PORT="${API_PORT:-3001}"
GO_BIN="${GO_BIN:-go}"
AUTH_TOKEN="${LENZ_DEV_AUTH_TOKEN:-demo-local-token}"
DATABASE_URL="${DATABASE_URL:-postgres://lenzcore:lenzcore123@localhost:${POSTGRES_PORT}/lenzcore?sslmode=disable}"
BASE_URL="http://localhost:${API_PORT}"
INSTITUTION_ID="11111111-1111-1111-1111-111111111111"
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
  curl -fsS -H "Authorization: Bearer ${AUTH_TOKEN}" "$@"
}

request_status() {
  local response_path="$1"
  shift
  curl -sS -o "$response_path" -w "%{http_code}" -H "Authorization: Bearer ${AUTH_TOKEN}" "$@"
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
  assert_balance_pair "$want" "$want"
}

assert_balance_pair() {
  assert_account_balance_pair "$ACCOUNT_ID" "$1" "$2"
}

assert_account_balance_pair() {
  local account_id="$1"
  local want_available="$2"
  local want_ledger="$3"
  local body
  body="$(request -H "X-Institution-ID: ${INSTITUTION_ID}" "${BASE_URL}/api/v1/accounts/${account_id}/balance")"
  assert_json "$body" ".available_minor == ${want_available} and .ledger_minor == ${want_ledger}" "expected account ${account_id} balance available=${want_available} ledger=${want_ledger}"
}

assert_ledger_status() {
  local transfer_json="$1"
  local ledger_status="$2"
  local reconciliation_status="$3"
  assert_json "$transfer_json" ".ledger_status == \"${ledger_status}\" and .reconciliation_status == \"${reconciliation_status}\"" "transfer status split mismatch"
}

assert_equal_id() {
  local body="$1"
  local want_id="$2"
  local message="$3"
  assert_json "$body" ".id == \"${want_id}\"" "$message"
}

assert_journal_balanced() {
  local journal_id="$1"
  local amount="$2"
  local body
  body="$(request -H "X-Institution-ID: ${INSTITUTION_ID}" "${BASE_URL}/api/v1/admin/ledger/journal/${journal_id}")"
  assert_json "$body" ".balanced == true" "journal ${journal_id} is not marked balanced"
  assert_json "$body" "([.postings[] | select(.direction == \"debit\") | .amount_minor] | add) == ${amount}" "journal ${journal_id} debit amount mismatch"
  assert_json "$body" "([.postings[] | select(.direction == \"credit\") | .amount_minor] | add) == ${amount}" "journal ${journal_id} credit amount mismatch"
}

sql_scalar() {
  docker exec lenzcore-postgres psql -U lenzcore -d lenzcore -tAc "$1"
}

assert_audit_action() {
  local action="$1"
  local count
  count="$(sql_scalar "SELECT COUNT(*) FROM audit_events WHERE institution_id = '${INSTITUTION_ID}' AND action = '${action}'")"
  [[ "$count" != "0" ]] || fail "missing audit event action: ${action}"
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

echo "Generating OpenAPI code..."
"$GO_BIN" generate ./apps/core/internal/corebanking
pass "OpenAPI code generated"

echo "Resetting Docker Compose services and volumes for a clean demo database..."
compose down -v --remove-orphans >/dev/null
compose up -d postgres redis >/dev/null
wait_container_healthy lenzcore-postgres
wait_container_healthy lenzcore-redis
pass "Docker Compose started healthy Postgres and Redis"

echo "Running migrations..."
DATABASE_URL="$DATABASE_URL" GO_BIN="$GO_BIN" ./scripts/migrate.sh up >/tmp/lenz-core-demo-migrate.log
cat /tmp/lenz-core-demo-migrate.log
pass "migrations applied"

echo "Running unit tests..."
"$GO_BIN" test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
pass "unit test suite passed"

echo "Running Postgres-backed integration tests..."
LENZ_INTEGRATION_DATABASE_URL="$DATABASE_URL" "$GO_BIN" test -count=1 -tags=integration ./apps/core/internal/corebanking -run 'TestSQL'
pass "SQL integration test suite passed"

TMP_PARENT="${TMPDIR:-/tmp}"
mkdir -p "$TMP_PARENT"
TMP_DIR="$(mktemp -d "${TMP_PARENT%/}/lenz-core-demo.XXXXXX")"
API_LOG="${TMP_DIR}/api.log"
"$GO_BIN" build -o "${TMP_DIR}/lenz-core-api" ./apps/core
DATABASE_URL="$DATABASE_URL" PORT="$API_PORT" APP_ENV=development LENZ_DEMO_MODE=true LENZ_DEV_AUTH_TOKEN="$AUTH_TOKEN" LENZ_DEV_INSTITUTION_ID="$INSTITUTION_ID" "${TMP_DIR}/lenz-core-api" >"$API_LOG" 2>&1 &
API_PID="$!"
wait_api
pass "API started successfully"

health="$(request "${BASE_URL}/api/v1/health")"
assert_json "$health" '.status == "ok"' "health endpoint did not return ok"
pass "GET /api/v1/health returned ok"

seed="$(request -X POST "${BASE_URL}/api/v1/demo/seed")"
assert_json "$seed" '.institution.id == "11111111-1111-1111-1111-111111111111" and .customer.id == "33333333-3333-3333-3333-333333333333" and .account.id == "44444444-4444-4444-4444-444444444444"' "demo seed response mismatch"
pass "POST /api/v1/demo/seed created demo tenant data"

accounts="$(request -H "X-Institution-ID: ${INSTITUTION_ID}" "${BASE_URL}/api/v1/customers/${CUSTOMER_ID}/accounts")"
assert_json "$accounts" 'length == 1 and .[0].id == "44444444-4444-4444-4444-444444444444" and .[0].account_number == "9990000001"' "customer accounts response mismatch"
pass "customer account lookup returned the demo account"

inbound="$(request -X POST "${BASE_URL}/api/v1/transfers/mock/inbound" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
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
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -H 'Idempotency-Key: demo-script-in-001' \
  -d "{\"account_id\":\"${ACCOUNT_ID}\",\"amount_minor\":500000,\"provider_event_id\":\"demo-script-provider-event-001\",\"provider_reference\":\"demo-script-in-ref-001\",\"narration\":\"Demo script duplicate idempotency\"}")"
assert_json "$dup_idempotency" ".id == \"${inbound_id}\"" "duplicate idempotency key returned a different transfer"
assert_balance 500000
pass "duplicate idempotency key did not double-credit"

dup_provider="$(request -X POST "${BASE_URL}/api/v1/transfers/mock/inbound" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -H 'Idempotency-Key: demo-script-in-002' \
  -d "{\"account_id\":\"${ACCOUNT_ID}\",\"amount_minor\":500000,\"provider_event_id\":\"demo-script-provider-event-001\",\"provider_reference\":\"demo-script-in-ref-001\",\"narration\":\"Demo script duplicate provider event\"}")"
assert_json "$dup_provider" ".id == \"${inbound_id}\"" "duplicate provider event returned a different transfer"
assert_balance 500000
pass "duplicate provider_event_id did not double-credit"

outbound="$(request -X POST "${BASE_URL}/api/v1/transfers/mock/outbound" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -H 'Idempotency-Key: demo-script-out-001' \
  -d "{\"account_id\":\"${ACCOUNT_ID}\",\"amount_minor\":125000,\"provider_reference\":\"demo-script-out-ref-001\",\"narration\":\"Demo script outbound\"}")"
assert_json "$outbound" '.status == "succeeded" and .amount_minor == 125000 and .journal_entry_id != null' "outbound transfer did not succeed with a journal"
assert_balance 375000
pass "mock outbound debited the account"

pending_out_failed="$(request -X POST "${BASE_URL}/api/v1/transfers/mock/outbound" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -H 'Idempotency-Key: demo-script-pending-out-failed-001' \
  -d "{\"account_id\":\"${ACCOUNT_ID}\",\"amount_minor\":50000,\"provider_reference\":\"demo-script-pending-out-failed-ref-001\",\"status\":\"pending\",\"narration\":\"Demo script pending outbound to fail\"}")"
assert_json "$pending_out_failed" '.status == "pending" and .journal_entry_id == null' "pending outbound-to-fail did not remain pending"
pending_out_failed_id="$(json_get '.id' "$pending_out_failed")"
assert_balance_pair 325000 375000
pass "pending outbound created a hold and reduced available balance only"

failed_pending_out="$(request -X POST "${BASE_URL}/api/v1/transfers/mock/outbound" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -H 'Idempotency-Key: demo-script-pending-out-failed-settle-001' \
  -d "{\"account_id\":\"${ACCOUNT_ID}\",\"amount_minor\":50000,\"provider_reference\":\"demo-script-pending-out-failed-ref-001\",\"status\":\"failed\",\"narration\":\"Demo script failed pending outbound\"}")"
assert_equal_id "$failed_pending_out" "$pending_out_failed_id" "failed pending outbound settlement created a new transfer"
assert_json "$failed_pending_out" '.status == "failed" and .journal_entry_id == null' "failed pending outbound did not release cleanly"
assert_balance 375000
pass "failed pending outbound released its hold without moving ledger money"

pending_out_success="$(request -X POST "${BASE_URL}/api/v1/transfers/mock/outbound" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -H 'Idempotency-Key: demo-script-pending-out-success-001' \
  -d "{\"account_id\":\"${ACCOUNT_ID}\",\"amount_minor\":25000,\"provider_reference\":\"demo-script-pending-out-success-ref-001\",\"status\":\"pending\",\"narration\":\"Demo script pending outbound to succeed\"}")"
assert_json "$pending_out_success" '.status == "pending" and .journal_entry_id == null' "pending outbound-to-succeed did not remain pending"
pending_out_success_id="$(json_get '.id' "$pending_out_success")"
assert_balance_pair 350000 375000
pass "second pending outbound reserved available balance"

succeeded_pending_out="$(request -X POST "${BASE_URL}/api/v1/transfers/mock/outbound" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -H 'Idempotency-Key: demo-script-pending-out-success-settle-001' \
  -d "{\"account_id\":\"${ACCOUNT_ID}\",\"amount_minor\":25000,\"provider_reference\":\"demo-script-pending-out-success-ref-001\",\"status\":\"succeeded\",\"narration\":\"Demo script succeeded pending outbound\"}")"
assert_equal_id "$succeeded_pending_out" "$pending_out_success_id" "successful pending outbound settlement created a new transfer"
assert_json "$succeeded_pending_out" '.status == "succeeded" and .journal_entry_id != null' "successful pending outbound did not post a journal"
succeeded_pending_out_journal_id="$(json_get '.journal_entry_id' "$succeeded_pending_out")"
assert_journal_balanced "$succeeded_pending_out_journal_id" 25000
assert_balance 350000
pass "successful pending outbound posted ledger entries and consumed its hold"

failed="$(request -X POST "${BASE_URL}/api/v1/transfers/mock/outbound" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -H 'Idempotency-Key: demo-script-failed-001' \
  -d "{\"account_id\":\"${ACCOUNT_ID}\",\"amount_minor\":999999999,\"narration\":\"Demo script insufficient funds\"}")"
assert_json "$failed" '.status == "failed" and .failure_reason == "insufficient_funds" and .journal_entry_id == null' "insufficient-funds transfer did not fail cleanly"
assert_balance 350000
failed_id="$(json_get '.id' "$failed")"
pass "insufficient-funds outbound failed without changing balance"

pending="$(request -X POST "${BASE_URL}/api/v1/transfers/mock/inbound" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -H 'Idempotency-Key: demo-script-pending-001' \
  -d "{\"account_id\":\"${ACCOUNT_ID}\",\"amount_minor\":100000,\"provider_event_id\":\"demo-script-provider-event-pending-001\",\"status\":\"pending\",\"narration\":\"Demo script pending\"}")"
assert_json "$pending" '.status == "pending" and .journal_entry_id == null' "pending transfer posted money or did not remain pending"
assert_balance 350000
pending_id="$(json_get '.id' "$pending")"
pass "pending transfer recorded without posting money"

reversal="$(request -X POST "${BASE_URL}/api/v1/transfers/${inbound_id}/reverse" -H "X-Institution-ID: ${INSTITUTION_ID}" -H 'Idempotency-Key: demo-script-reversal-001')"
assert_json "$reversal" ".direction == \"reversal\" and .status == \"succeeded\" and .reversal_of_transfer_id == \"${inbound_id}\" and .journal_entry_id != null" "reversal did not create a succeeded reversal transfer"
assert_ledger_status "$reversal" "reversal_deficit" "manual_review"
reversal_id="$(json_get '.id' "$reversal")"
reversal_journal_id="$(json_get '.journal_entry_id' "$reversal")"
assert_journal_balanced "$reversal_journal_id" 500000
assert_balance -150000
pass "reversal created a new transfer, balanced journal entry, and manual-review deficit"

transactions="$(request -H "X-Institution-ID: ${INSTITUTION_ID}" "${BASE_URL}/api/v1/accounts/${ACCOUNT_ID}/transactions")"
assert_json "$transactions" 'length == 7' "transaction history did not contain seven Lenz transfer rows"
assert_json "$transactions" "[.[] | select(.transfer_id == \"${inbound_id}\" and .direction == \"credit\" and .status == \"succeeded\" and .signed_amount_minor == 500000 and .journal_entry_id != null)] | length == 1" "history missing posted inbound credit row"
assert_json "$transactions" "[.[] | select(.direction == \"debit\" and .status == \"succeeded\" and .signed_amount_minor == -125000 and .journal_entry_id != null)] | length == 1" "history missing posted outbound debit row"
assert_json "$transactions" "[.[] | select(.transfer_id == \"${pending_out_failed_id}\" and .status == \"failed\" and .signed_amount_minor == 0 and .journal_entry_id == null)] | length == 1" "history missing failed held outbound row"
assert_json "$transactions" "[.[] | select(.transfer_id == \"${pending_out_success_id}\" and .status == \"succeeded\" and .signed_amount_minor == -25000 and .journal_entry_id != null)] | length == 1" "history missing settled held outbound row"
assert_json "$transactions" "[.[] | select(.transfer_id == \"${pending_id}\" and .status == \"pending\" and .signed_amount_minor == 0 and .journal_entry_id == null)] | length == 1" "history missing pending row"
assert_json "$transactions" "[.[] | select(.transfer_id == \"${failed_id}\" and .status == \"failed\" and .signed_amount_minor == 0 and .journal_entry_id == null)] | length == 1" "history missing failed row"
assert_json "$transactions" "[.[] | select(.transfer_id == \"${reversal_id}\" and .direction == \"debit\" and .signed_amount_minor == -500000 and .journal_entry_id != null)] | length == 1" "history missing reversal debit row"
pass "transaction history came from Lenz transfer/journal/posting records"

control_customer="$(request -X POST "${BASE_URL}/api/v1/customers" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d "{\"branch_id\":\"${DEMO_BRANCH_ID:-22222222-2222-2222-2222-222222222222}\",\"customer_type\":\"individual\",\"first_name\":\"Control\",\"last_name\":\"Demo\",\"email\":\"control.demo@example.com\",\"phone\":\"+2348000000001\"}")"
control_customer_id="$(json_get '.id' "$control_customer")"
control_account="$(request -X POST "${BASE_URL}/api/v1/accounts" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d "{\"customer_id\":\"${control_customer_id}\",\"account_number\":\"9990000101\",\"name\":\"Control Demo Wallet\",\"product_type\":\"standard_wallet\",\"currency_id\":\"NGN\"}")"
control_account_id="$(json_get '.id' "$control_account")"
transfer_customer="$(request -X POST "${BASE_URL}/api/v1/customers" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d "{\"branch_id\":\"${DEMO_BRANCH_ID:-22222222-2222-2222-2222-222222222222}\",\"customer_type\":\"individual\",\"first_name\":\"Transfer\",\"last_name\":\"Demo\",\"email\":\"transfer.demo@example.com\",\"phone\":\"+2348000000002\"}")"
transfer_customer_id="$(json_get '.id' "$transfer_customer")"
transfer_account="$(request -X POST "${BASE_URL}/api/v1/accounts" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d "{\"customer_id\":\"${transfer_customer_id}\",\"account_number\":\"9990000102\",\"name\":\"Transfer Demo Wallet\",\"product_type\":\"standard_wallet\",\"currency_id\":\"NGN\"}")"
transfer_account_id="$(json_get '.id' "$transfer_account")"
control_credit="$(request -X POST "${BASE_URL}/api/v1/internal/credits" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d "{\"account_id\":\"${control_account_id}\",\"amount_minor\":50000,\"currency_id\":\"NGN\",\"idempotency_key\":\"demo-controls-credit-001\",\"reference\":\"demo-controls-credit-ref\"}")"
assert_json "$control_credit" '.status == "succeeded" and .journal_entry_id != null' "control account funding failed"
control_credit_id="$(json_get '.id' "$control_credit")"
control_credit_journal_id="$(json_get '.journal_entry_id' "$control_credit")"
assert_account_balance_pair "$control_account_id" 50000 50000
pass "control test account was created and funded"

lien="$(request -X POST "${BASE_URL}/api/v1/accounts/${control_account_id}/liens" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d '{"amount_minor":15000,"currency_id":"NGN","reference":"demo-lien-ref-001","reason":"demo lien"}')"
lien_id="$(json_get '.id' "$lien")"
assert_json "$lien" '.status == "active" and .transfer_id == null and .amount_minor == 15000 and .reference == "demo-lien-ref-001"' "lien placement response mismatch"
assert_account_balance_pair "$control_account_id" 35000 50000
lien_db="$(docker exec lenzcore-postgres psql -U lenzcore -d lenzcore -tAc "SELECT status || '|' || amount_minor || '|' || reference || '|' || CASE WHEN transfer_id IS NULL THEN 'null_transfer' ELSE 'linked_transfer' END FROM account_holds WHERE id = '${lien_id}'")"
[[ "$lien_db" == "active|15000|demo-lien-ref-001|null_transfer" ]] || fail "lien DB row mismatch: ${lien_db}"
pass "lien reduced available balance and persisted an active hold row"

control_fail_path="${TMP_DIR}/control-fail.json"
control_debit_status="$(request_status "$control_fail_path" -X POST "${BASE_URL}/api/v1/internal/debits" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d "{\"account_id\":\"${control_account_id}\",\"amount_minor\":40000,\"currency_id\":\"NGN\",\"idempotency_key\":\"demo-controls-over-lien-debit\"}")"
[[ "$control_debit_status" == "422" ]] || fail "expected lien-limited debit to return 422, got ${control_debit_status}: $(cat "$control_fail_path")"
pass "debit above lien-reduced available balance was rejected"

released_lien="$(request -X DELETE "${BASE_URL}/api/v1/accounts/${control_account_id}/liens/${lien_id}" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d '{"reference":"demo-lien-release-ref-001"}')"
assert_json "$released_lien" '.status == "released" and .released_at != null' "lien release response mismatch"
assert_account_balance_pair "$control_account_id" 50000 50000
pass "lien release restored available balance without deleting the hold"

control_debit="$(request -X POST "${BASE_URL}/api/v1/internal/debits" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d "{\"account_id\":\"${control_account_id}\",\"amount_minor\":40000,\"currency_id\":\"NGN\",\"idempotency_key\":\"demo-controls-post-lien-debit\",\"reference\":\"demo-controls-post-lien-debit-ref\"}")"
assert_json "$control_debit" '.status == "succeeded" and .journal_entry_id != null' "post-lien debit did not succeed"
control_debit_id="$(json_get '.id' "$control_debit")"
assert_account_balance_pair "$control_account_id" 10000 10000
pass "debit succeeded after lien release"

control_transfer="$(request -X POST "${BASE_URL}/api/v1/internal/transfers" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d "{\"source_account_id\":\"${control_account_id}\",\"destination_account_id\":\"${transfer_account_id}\",\"amount_minor\":5000,\"currency_id\":\"NGN\",\"idempotency_key\":\"demo-controls-transfer-001\",\"reference\":\"demo-controls-transfer-ref\"}")"
assert_json "$control_transfer" '.status == "succeeded" and .journal_entry_id != null' "internal transfer did not succeed"
control_transfer_id="$(json_get '.id' "$control_transfer")"
control_transfer_journal_id="$(json_get '.journal_entry_id' "$control_transfer")"
assert_account_balance_pair "$control_account_id" 5000 5000
assert_account_balance_pair "$transfer_account_id" 5000 5000
pass "internal transfer moved money between customer accounts"

frozen_account="$(request -X POST "${BASE_URL}/api/v1/accounts/${control_account_id}/freeze" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d '{"reference":"demo-freeze-ref-001","reason":"Authorization: Bearer demo-secret-token"}')"
assert_json "$frozen_account" '.status == "frozen"' "freeze did not mark account frozen"
control_credit_status="$(request_status "$control_fail_path" -X POST "${BASE_URL}/api/v1/internal/credits" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d "{\"account_id\":\"${control_account_id}\",\"amount_minor\":1000,\"currency_id\":\"NGN\",\"idempotency_key\":\"demo-controls-frozen-credit\"}")"
[[ "$control_credit_status" == "400" ]] || fail "expected frozen credit to return 400, got ${control_credit_status}: $(cat "$control_fail_path")"
control_debit_status="$(request_status "$control_fail_path" -X POST "${BASE_URL}/api/v1/internal/debits" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d "{\"account_id\":\"${control_account_id}\",\"amount_minor\":1000,\"currency_id\":\"NGN\",\"idempotency_key\":\"demo-controls-frozen-debit\"}")"
[[ "$control_debit_status" == "400" ]] || fail "expected frozen debit to return 400, got ${control_debit_status}: $(cat "$control_fail_path")"
pass "frozen account blocked credits and debits"

active_account="$(request -X POST "${BASE_URL}/api/v1/accounts/${control_account_id}/unfreeze" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d '{"reference":"demo-unfreeze-ref-001","reason":"demo unfreeze"}')"
assert_json "$active_account" '.status == "active"' "unfreeze did not mark account active"
pnd_account="$(request -X POST "${BASE_URL}/api/v1/accounts/${control_account_id}/post-no-debit" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d '{"reference":"demo-pnd-ref-001","reason":"demo pnd"}')"
assert_json "$pnd_account" '.status == "post_no_debit"' "PND did not mark account post_no_debit"
control_debit_status="$(request_status "$control_fail_path" -X POST "${BASE_URL}/api/v1/internal/debits" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d "{\"account_id\":\"${control_account_id}\",\"amount_minor\":1000,\"currency_id\":\"NGN\",\"idempotency_key\":\"demo-controls-pnd-debit\"}")"
[[ "$control_debit_status" == "400" ]] || fail "expected PND debit to return 400, got ${control_debit_status}: $(cat "$control_fail_path")"
pnd_credit="$(request -X POST "${BASE_URL}/api/v1/internal/credits" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d "{\"account_id\":\"${control_account_id}\",\"amount_minor\":1000,\"currency_id\":\"NGN\",\"idempotency_key\":\"demo-controls-pnd-credit\",\"reference\":\"demo-controls-pnd-credit-ref\"}")"
assert_json "$pnd_credit" '.status == "succeeded" and .journal_entry_id != null' "PND account did not allow credit"
pass "PND blocked debits while allowing credits"

pnd_off_account="$(request -X DELETE "${BASE_URL}/api/v1/accounts/${control_account_id}/post-no-debit" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d '{"reference":"demo-pnd-off-ref-001","reason":"demo pnd clear"}')"
assert_json "$pnd_off_account" '.status == "active"' "PND deactivation did not mark account active"
pass "PND deactivation restored active account status"

external_outbound="$(request -X POST "${BASE_URL}/api/v1/external/transfers/outbound" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d "{\"source_account_id\":\"${control_account_id}\",\"destination_institution_code\":\"999001\",\"destination_account_number\":\"9990000001\",\"destination_account_name\":\"Ada Demo Wallet\",\"amount_minor\":1000,\"currency_id\":\"NGN\",\"idempotency_key\":\"demo-external-outbound-success\",\"reference\":\"demo-external-outbound-success-ref\",\"narration\":\"Demo external outbound\",\"scenario\":\"success\"}")"
external_outbound_journal_id="$(json_get '.journal_entry_id' "$external_outbound")"
assert_json "$external_outbound" '.status == "succeeded" and .provider_status == "succeeded" and .ledger_status == "posted" and .reconciliation_status == "matched" and .journal_entry_id != null and .hold_id != null' "external outbound success mismatch"
assert_account_balance_pair "$control_account_id" 5000 5000
assert_journal_balanced "$external_outbound_journal_id" 1000
pass "external outbound success consumed its hold and posted a balanced journal"

external_unknown="$(request -X POST "${BASE_URL}/api/v1/external/transfers/outbound" \
  -H 'Content-Type: application/json' \
  -H "X-Institution-ID: ${INSTITUTION_ID}" \
  -d "{\"source_account_id\":\"${control_account_id}\",\"destination_institution_code\":\"999001\",\"destination_account_number\":\"9990000001\",\"amount_minor\":500,\"currency_id\":\"NGN\",\"idempotency_key\":\"demo-external-outbound-unknown\",\"reference\":\"demo-external-outbound-unknown-ref\",\"narration\":\"Demo external outbound unknown\",\"scenario\":\"provider_unknown\"}")"
external_unknown_id="$(json_get '.transfer_id' "$external_unknown")"
assert_json "$external_unknown" '.status == "pending" and .provider_status == "provider_unknown" and .ledger_status == "pending" and .reconciliation_status == "manual_review" and .journal_entry_id == null and .hold_id != null' "external provider_unknown mismatch"
assert_account_balance_pair "$control_account_id" 4500 5000
reconciliation_items="$(request -H "X-Institution-ID: ${INSTITUTION_ID}" "${BASE_URL}/api/v1/admin/reconciliation-items?provider_status=provider_unknown")"
assert_json "$reconciliation_items" "[.[] | select(.transfer_id == \"${external_unknown_id}\" and .review_reason == \"provider_unknown\" and .recommended_next_action == \"requery_provider\")] | length == 1" "external provider_unknown was not surfaced in reconciliation queue"
pass "external provider_unknown kept a hold without posting and entered reconciliation"

transfers="$(request -H "X-Institution-ID: ${INSTITUTION_ID}" "${BASE_URL}/api/v1/admin/transfers")"
assert_json "$transfers" 'length >= 13' "admin transfer list did not include demo and account-control transfer records"
pass "admin transfer list returned all demo transfers"

assert_audit_action "customer.created"
assert_audit_action "account.created"
assert_audit_action "internal_credit.posted"
assert_audit_action "internal_debit.posted"
assert_audit_action "internal_transfer.posted"
assert_audit_action "account.frozen"
assert_audit_action "account.unfrozen"
assert_audit_action "account.pnd_activated"
assert_audit_action "account.pnd_deactivated"
assert_audit_action "account.lien_placed"
assert_audit_action "account.lien_released"
assert_audit_action "external_outbound.succeeded"
assert_audit_action "external_outbound.provider_unknown"
audit_link_count="$(sql_scalar "SELECT COUNT(*) FROM audit_events WHERE institution_id = '${INSTITUTION_ID}' AND action = 'internal_credit.posted' AND account_id = '${control_account_id}' AND transfer_id = '${control_credit_id}' AND journal_entry_id = '${control_credit_journal_id}'")"
[[ "$audit_link_count" == "1" ]] || fail "internal credit audit link mismatch: ${audit_link_count}"
transfer_audit_link_count="$(sql_scalar "SELECT COUNT(*) FROM audit_events WHERE institution_id = '${INSTITUTION_ID}' AND action = 'internal_transfer.posted' AND account_id = '${control_account_id}' AND transfer_id = '${control_transfer_id}' AND journal_entry_id = '${control_transfer_journal_id}'")"
[[ "$transfer_audit_link_count" == "1" ]] || fail "internal transfer audit link mismatch: ${transfer_audit_link_count}"
debit_audit_link_count="$(sql_scalar "SELECT COUNT(*) FROM audit_events WHERE institution_id = '${INSTITUTION_ID}' AND action = 'internal_debit.posted' AND account_id = '${control_account_id}' AND transfer_id = '${control_debit_id}'")"
[[ "$debit_audit_link_count" == "1" ]] || fail "internal debit audit link mismatch: ${debit_audit_link_count}"
unsafe_audit_metadata_count="$(sql_scalar "SELECT COUNT(*) FROM audit_events WHERE institution_id = '${INSTITUTION_ID}' AND (metadata::text ILIKE '%authorization%' OR metadata::text ILIKE '%bearer %' OR metadata::text ILIKE '%token%' OR metadata::text ILIKE '%secret%' OR metadata::text ILIKE '%password%' OR metadata::text ILIKE '%bvn%' OR metadata::text ILIKE '%nin%')")"
[[ "$unsafe_audit_metadata_count" == "0" ]] || fail "unsafe audit metadata persisted: ${unsafe_audit_metadata_count}"
pass "audit events were persisted with tenant, account, transfer, journal, and sanitized metadata checks"

echo
echo "DEMO TRANSFER SPINE: PASS"
