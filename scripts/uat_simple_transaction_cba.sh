#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

POSTGRES_PORT="${POSTGRES_PORT:-55432}"
API_PORT="${API_PORT:-3001}"
GO_BIN="${GO_BIN:-go}"
AUTH_TOKEN="${LENZ_DEV_AUTH_TOKEN:-local-dev-token}"
INSTITUTION_ID="${INSTITUTION_ID:-11111111-1111-1111-1111-111111111111}"
BRANCH_ID="${BRANCH_ID:-22222222-2222-2222-2222-222222222222}"
POSTGRES_USER="${POSTGRES_USER:-lenzcore}"
DB_CONTAINER="${DB_CONTAINER:-lenzcore-postgres}"
BASE_URL="http://localhost:${API_PORT}"

UAT_DB="lenzcore_uat_$(date +%s)_$$"
DATABASE_URL="postgres://lenzcore:lenzcore123@localhost:${POSTGRES_PORT}/${UAT_DB}?sslmode=disable"
API_PID=""
TMP_DIR=""
API_LOG=""

# shellcheck source=scripts/lib/cba_shell.sh
source "$ROOT_DIR/scripts/lib/cba_shell.sh"

cleanup() {
  if [[ -n "$API_PID" ]]; then
    kill "$API_PID" >/dev/null 2>&1 || true
    wait "$API_PID" >/dev/null 2>&1 || true
  fi
  if docker inspect "$DB_CONTAINER" >/dev/null 2>&1; then
    docker exec "$DB_CONTAINER" dropdb -U "$POSTGRES_USER" "$UAT_DB" >/dev/null 2>&1 || true
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
  curl -fsS \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -H "X-Institution-ID: ${INSTITUTION_ID}" \
    "$@"
}

request_status() {
  local response_path="$1"
  shift
  curl -sS -o "$response_path" -w "%{http_code}" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -H "X-Institution-ID: ${INSTITUTION_ID}" \
    "$@"
}

sql_scalar() {
  docker exec "$DB_CONTAINER" psql -U "$POSTGRES_USER" -d "$UAT_DB" -tAc "$1"
}

sql_exec() {
  docker exec "$DB_CONTAINER" psql -U "$POSTGRES_USER" -d "$UAT_DB" -P pager=off -c "$1"
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
  local account_id="$1"
  local want_available="$2"
  local want_ledger="$3"
  local body
  body="$(request "${BASE_URL}/api/v1/accounts/${account_id}/balance")"
  assert_json "$body" ".account_id == \"${account_id}\" and .available_minor == ${want_available} and .ledger_minor == ${want_ledger}" "balance mismatch for account ${account_id}"
}

assert_journal_balanced() {
  local journal_id="$1"
  local amount="$2"
  local body
  body="$(request "${BASE_URL}/api/v1/admin/ledger/journal/${journal_id}")"
  assert_json "$body" ".balanced == true" "journal ${journal_id} is not balanced"
  assert_json "$body" "([.postings[] | select(.direction == \"debit\") | .amount_minor] | add) == ${amount}" "journal ${journal_id} debit total mismatch"
  assert_json "$body" "([.postings[] | select(.direction == \"credit\") | .amount_minor] | add) == ${amount}" "journal ${journal_id} credit total mismatch"
}

assert_history_row() {
  local account_id="$1"
  local transfer_id="$2"
  local direction="$3"
  local signed_amount="$4"
  local body
  body="$(request "${BASE_URL}/api/v1/accounts/${account_id}/transactions?limit=50")"
  assert_json "$body" "[.[] | select(.transfer_id == \"${transfer_id}\" and .direction == \"${direction}\" and .signed_amount_minor == ${signed_amount} and .status == \"succeeded\" and .journal_entry_id != null)] | length == 1" "history missing ${direction} ${signed_amount} for ${account_id}"
}

assert_history_state() {
  local account_id="$1"
  local transfer_id="$2"
  local status="$3"
  local provider_status="$4"
  local ledger_status="$5"
  local reconciliation_status="$6"
  local signed_amount="$7"
  local journal_filter="$8"
  local body
  body="$(request "${BASE_URL}/api/v1/accounts/${account_id}/transactions?limit=50")"
  assert_json "$body" "[.[] | select(.transfer_id == \"${transfer_id}\" and .status == \"${status}\" and .provider_status == \"${provider_status}\" and .ledger_status == \"${ledger_status}\" and .reconciliation_status == \"${reconciliation_status}\" and .signed_amount_minor == ${signed_amount} and .journal_entry_id ${journal_filter})] | length == 1" "history missing transfer ${transfer_id} with status ${status}/${provider_status}"
}

assert_transfer_hold_status() {
  local transfer_id="$1"
  local want_status="$2"
  local status
  status="$(sql_scalar "SELECT status FROM account_holds WHERE institution_id = '${INSTITUTION_ID}' AND transfer_id = '${transfer_id}'")"
  [[ "$status" == "$want_status" ]] || fail "hold for transfer ${transfer_id} status=${status}, want ${want_status}"
}

assert_transfer_journal_count() {
  local transfer_id="$1"
  local want_count="$2"
  local count
  count="$(sql_scalar "SELECT COUNT(*) FROM journal_entries WHERE institution_id = '${INSTITUTION_ID}' AND transfer_id = '${transfer_id}'")"
  [[ "$count" == "$want_count" ]] || fail "journal count for transfer ${transfer_id}=${count}, want ${want_count}"
}

assert_reconciliation_item() {
  local transfer_id="$1"
  local review_reason="$2"
  local action="$3"
  local body
  body="$(request "${BASE_URL}/api/v1/admin/reconciliation-items?limit=100")"
  assert_json "$body" "[.[] | select(.transfer_id == \"${transfer_id}\" and .review_reason == \"${review_reason}\" and .recommended_next_action == \"${action}\")] | length == 1" "missing reconciliation item ${transfer_id}"
}

assert_blocked_money_request() {
  local message="$1"
  shift
  local response_path="${TMP_DIR}/blocked.json"
  local http_code
  http_code="$(request_status "$response_path" "$@")"
  [[ "$http_code" == "400" ]] || fail "${message}: expected HTTP 400, got ${http_code} body=$(cat "$response_path")"
  assert_json "$(cat "$response_path")" '.message == "invalid_request"' "${message}: expected invalid_request body"
}

assert_audit_action_count() {
  local action="$1"
  local want_min="$2"
  local count
  count="$(sql_scalar "SELECT COUNT(*) FROM audit_events WHERE institution_id = '${INSTITUTION_ID}' AND action = '${action}'")"
  [[ "$count" -ge "$want_min" ]] || fail "audit action ${action} count=${count}, want at least ${want_min}"
}

assert_no_transfers_for_keys() {
  local keys_sql="$1"
  local count
  count="$(sql_scalar "SELECT COUNT(*) FROM transfers WHERE institution_id = '${INSTITUTION_ID}' AND idempotency_key IN (${keys_sql})")"
  [[ "$count" == "0" ]] || fail "blocked requests created ${count} transfer rows for keys ${keys_sql}"
}

assert_journal_reconciliation() {
  local unbalanced_count
  unbalanced_count="$(sql_scalar "
WITH journal_totals AS (
  SELECT
    je.id,
    je.total_debit_minor,
    je.total_credit_minor,
    COALESCE(SUM(p.amount_minor) FILTER (WHERE p.direction = 'debit'), 0) AS posting_debit_minor,
    COALESCE(SUM(p.amount_minor) FILTER (WHERE p.direction = 'credit'), 0) AS posting_credit_minor
  FROM journal_entries je
  LEFT JOIN postings p ON p.institution_id = je.institution_id AND p.journal_entry_id = je.id
  WHERE je.institution_id = '${INSTITUTION_ID}'
  GROUP BY je.id, je.total_debit_minor, je.total_credit_minor
)
SELECT COUNT(*)
FROM journal_totals
WHERE total_debit_minor <> total_credit_minor
   OR total_debit_minor <> posting_debit_minor
   OR total_credit_minor <> posting_credit_minor;")"
  [[ "$unbalanced_count" == "0" ]] || fail "found ${unbalanced_count} unreconciled journal entries"

  local balance_mismatch_count
  balance_mismatch_count="$(sql_scalar "
WITH posting_balances AS (
  SELECT
    a.id AS account_id,
    COALESCE(SUM(
      CASE
        WHEN a.normal_balance = 'credit' AND p.direction = 'credit' THEN p.amount_minor
        WHEN a.normal_balance = 'credit' AND p.direction = 'debit' THEN -p.amount_minor
        WHEN a.normal_balance = 'debit' AND p.direction = 'debit' THEN p.amount_minor
        WHEN a.normal_balance = 'debit' AND p.direction = 'credit' THEN -p.amount_minor
        ELSE 0
      END
    ), 0) AS ledger_minor
  FROM accounts a
  LEFT JOIN postings p ON p.institution_id = a.institution_id AND p.account_id = a.id
  WHERE a.institution_id = '${INSTITUTION_ID}'
  GROUP BY a.id
)
SELECT COUNT(*)
FROM account_balances b
JOIN posting_balances pb ON pb.account_id = b.account_id
WHERE b.institution_id = '${INSTITUTION_ID}' AND b.ledger_minor <> pb.ledger_minor;")"
  [[ "$balance_mismatch_count" == "0" ]] || fail "found ${balance_mismatch_count} account ledger balances that do not reconcile to postings"
}

ensure_docker_running
require_cmd curl
require_cmd jq
require_cmd "$GO_BIN"
require_free_port "$API_PORT"

echo "Starting Postgres for UAT..."
compose up -d postgres >/dev/null
wait_container_healthy "$DB_CONTAINER"
docker exec "$DB_CONTAINER" createdb -U "$POSTGRES_USER" "$UAT_DB"
pass "temporary UAT database created: ${UAT_DB}"

echo "Generating OpenAPI code..."
"$GO_BIN" generate ./apps/core/internal/corebanking
pass "OpenAPI code generated"

echo "Bootstrapping CBA v0.1 tenant..."
DATABASE_URL="$DATABASE_URL" POSTGRES_DB="$UAT_DB" START_DOCKER=false ./scripts/bootstrap_cba_v0_1.sh >/tmp/lenz-uat-bootstrap.log
pass "institution, branch, and internal settlement account bootstrapped"

TMP_PARENT="${TMPDIR:-/tmp}"
mkdir -p "$TMP_PARENT"
TMP_DIR="$(mktemp -d "${TMP_PARENT%/}/lenz-core-uat.XXXXXX")"
API_LOG="${TMP_DIR}/api.log"

echo "Building and starting API..."
"$GO_BIN" build -o "${TMP_DIR}/lenz-core-api" ./apps/core
DATABASE_URL="$DATABASE_URL" \
PORT="$API_PORT" \
APP_ENV=development \
LENZ_DEMO_MODE=false \
LENZ_DEV_AUTH_TOKEN="$AUTH_TOKEN" \
LENZ_DEV_INSTITUTION_ID="$INSTITUTION_ID" \
"${TMP_DIR}/lenz-core-api" >"$API_LOG" 2>&1 &
API_PID="$!"
wait_api
pass "API started with demo mode disabled"

health="$(request "${BASE_URL}/api/v1/health")"
assert_json "$health" '.status == "ok"' "health check failed"
pass "health check passed"

customer_a="$(request -X POST "${BASE_URL}/api/v1/customers" \
  -H "Content-Type: application/json" \
  -d "{\"branch_id\":\"${BRANCH_ID}\",\"customer_type\":\"individual\",\"first_name\":\"UAT\",\"last_name\":\"Primary\",\"email\":\"uat.primary@example.com\",\"phone\":\"+2348000000101\"}")"
customer_a_id="$(json_get '.id' "$customer_a")"
assert_json "$customer_a" ".institution_id == \"${INSTITUTION_ID}\" and .branch_id == \"${BRANCH_ID}\" and .status == \"active\"" "customer A creation mismatch"
pass "created first customer"

account_a="$(request -X POST "${BASE_URL}/api/v1/accounts" \
  -H "Content-Type: application/json" \
  -d "{\"customer_id\":\"${customer_a_id}\",\"account_number\":\"1234567890\",\"name\":\"UAT Primary Wallet\",\"product_type\":\"standard_wallet\",\"currency_id\":\"NGN\"}")"
account_a_id="$(json_get '.id' "$account_a")"
assert_json "$account_a" '.account_number == "1234567890" and .kind == "customer" and .status == "active"' "account A creation mismatch"
assert_balance "$account_a_id" 0 0
pass "created first account with zero balance"

credit="$(request -X POST "${BASE_URL}/api/v1/internal/credits" \
  -H "Content-Type: application/json" \
  -d "{\"account_id\":\"${account_a_id}\",\"amount_minor\":1000000,\"currency_id\":\"NGN\",\"idempotency_key\":\"uat-credit-001\",\"narration\":\"UAT cash deposit\",\"reference\":\"uat-credit-ref-001\"}")"
credit_id="$(json_get '.id' "$credit")"
credit_journal_id="$(json_get '.journal_entry_id' "$credit")"
assert_json "$credit" '.status == "succeeded" and .ledger_status == "posted" and .reconciliation_status == "matched" and .amount_minor == 1000000 and .journal_entry_id != null' "credit transfer mismatch"
assert_balance "$account_a_id" 1000000 1000000
assert_history_row "$account_a_id" "$credit_id" "credit" 1000000
assert_journal_balanced "$credit_journal_id" 1000000
pass "credit posted, history recorded, and journal balanced"

debit="$(request -X POST "${BASE_URL}/api/v1/internal/debits" \
  -H "Content-Type: application/json" \
  -d "{\"account_id\":\"${account_a_id}\",\"amount_minor\":250000,\"currency_id\":\"NGN\",\"idempotency_key\":\"uat-debit-001\",\"narration\":\"UAT cash withdrawal\",\"reference\":\"uat-debit-ref-001\"}")"
debit_id="$(json_get '.id' "$debit")"
debit_journal_id="$(json_get '.journal_entry_id' "$debit")"
assert_json "$debit" '.status == "succeeded" and .ledger_status == "posted" and .reconciliation_status == "matched" and .amount_minor == 250000 and .journal_entry_id != null' "debit transfer mismatch"
assert_balance "$account_a_id" 750000 750000
assert_history_row "$account_a_id" "$debit_id" "debit" -250000
assert_journal_balanced "$debit_journal_id" 250000
pass "debit posted, history recorded, and journal balanced"

customer_b="$(request -X POST "${BASE_URL}/api/v1/customers" \
  -H "Content-Type: application/json" \
  -d "{\"branch_id\":\"${BRANCH_ID}\",\"customer_type\":\"individual\",\"first_name\":\"UAT\",\"last_name\":\"Receiver\",\"email\":\"uat.receiver@example.com\",\"phone\":\"+2348000000102\"}")"
customer_b_id="$(json_get '.id' "$customer_b")"
account_b="$(request -X POST "${BASE_URL}/api/v1/accounts" \
  -H "Content-Type: application/json" \
  -d "{\"customer_id\":\"${customer_b_id}\",\"account_number\":\"2234567890\",\"name\":\"UAT Receiver Wallet\",\"product_type\":\"standard_wallet\",\"currency_id\":\"NGN\"}")"
account_b_id="$(json_get '.id' "$account_b")"
assert_json "$account_b" '.account_number == "2234567890" and .kind == "customer" and .status == "active"' "account B creation mismatch"
assert_balance "$account_b_id" 0 0
pass "created second customer/account"

transfer="$(request -X POST "${BASE_URL}/api/v1/internal/transfers" \
  -H "Content-Type: application/json" \
  -d "{\"source_account_id\":\"${account_a_id}\",\"destination_account_id\":\"${account_b_id}\",\"amount_minor\":300000,\"currency_id\":\"NGN\",\"idempotency_key\":\"uat-transfer-001\",\"narration\":\"UAT internal transfer\",\"reference\":\"uat-transfer-ref-001\"}")"
transfer_id="$(json_get '.id' "$transfer")"
transfer_journal_id="$(json_get '.journal_entry_id' "$transfer")"
assert_json "$transfer" '.status == "succeeded" and .ledger_status == "posted" and .reconciliation_status == "matched" and .amount_minor == 300000 and .journal_entry_id != null' "internal transfer mismatch"
assert_balance "$account_a_id" 450000 450000
assert_balance "$account_b_id" 300000 300000
assert_history_row "$account_a_id" "$transfer_id" "debit" -300000
assert_history_row "$account_b_id" "$transfer_id" "credit" 300000
assert_journal_balanced "$transfer_journal_id" 300000
pass "transfer posted with matching history on both accounts"

external_success="$(request -X POST "${BASE_URL}/api/v1/external/transfers/outbound" \
  -H "Content-Type: application/json" \
  -d "{\"source_account_id\":\"${account_a_id}\",\"destination_institution_code\":\"999001\",\"destination_account_number\":\"9990000001\",\"destination_account_name\":\"Ada Demo Wallet\",\"amount_minor\":25000,\"currency_id\":\"NGN\",\"idempotency_key\":\"uat-external-out-success\",\"narration\":\"UAT external outbound success\",\"reference\":\"uat-external-out-success-ref\",\"scenario\":\"success\"}")"
external_success_id="$(json_get '.transfer_id' "$external_success")"
external_success_journal_id="$(json_get '.journal_entry_id' "$external_success")"
assert_json "$external_success" '.status == "succeeded" and .provider_status == "succeeded" and .ledger_status == "posted" and .reconciliation_status == "matched" and .journal_entry_id != null and .hold_id != null' "external outbound success mismatch"
assert_balance "$account_a_id" 425000 425000
assert_transfer_hold_status "$external_success_id" "consumed"
assert_journal_balanced "$external_success_journal_id" 25000
assert_history_state "$account_a_id" "$external_success_id" "succeeded" "succeeded" "posted" "matched" -25000 "!= null"
pass "external outbound success posted ledger after consuming hold"

external_failed="$(request -X POST "${BASE_URL}/api/v1/external/transfers/outbound" \
  -H "Content-Type: application/json" \
  -d "{\"source_account_id\":\"${account_a_id}\",\"destination_institution_code\":\"999001\",\"destination_account_number\":\"9990000001\",\"amount_minor\":12000,\"currency_id\":\"NGN\",\"idempotency_key\":\"uat-external-out-failed\",\"narration\":\"UAT external outbound failed\",\"reference\":\"uat-external-out-failed-ref\",\"scenario\":\"failed\"}")"
external_failed_id="$(json_get '.transfer_id' "$external_failed")"
assert_json "$external_failed" '.status == "failed" and .provider_status == "failed" and .ledger_status == "no_posting" and .reconciliation_status == "no_action" and .journal_entry_id == null and .hold_id != null' "external outbound failed mismatch"
assert_balance "$account_a_id" 425000 425000
assert_transfer_hold_status "$external_failed_id" "released"
assert_transfer_journal_count "$external_failed_id" 0
assert_history_state "$account_a_id" "$external_failed_id" "failed" "failed" "no_posting" "no_action" 0 "== null"
pass "external outbound failure released hold without a journal"

external_pending="$(request -X POST "${BASE_URL}/api/v1/external/transfers/outbound" \
  -H "Content-Type: application/json" \
  -d "{\"source_account_id\":\"${account_a_id}\",\"destination_institution_code\":\"999001\",\"destination_account_number\":\"9990000001\",\"amount_minor\":10000,\"currency_id\":\"NGN\",\"idempotency_key\":\"uat-external-out-pending\",\"narration\":\"UAT external outbound pending\",\"reference\":\"uat-external-out-pending-ref\",\"scenario\":\"pending\"}")"
external_pending_id="$(json_get '.transfer_id' "$external_pending")"
assert_json "$external_pending" '.status == "pending" and .provider_status == "pending" and .ledger_status == "pending" and .reconciliation_status == "pending" and .journal_entry_id == null and .hold_id != null' "external outbound pending mismatch"
assert_balance "$account_a_id" 415000 425000
assert_transfer_hold_status "$external_pending_id" "active"
assert_transfer_journal_count "$external_pending_id" 0
assert_history_state "$account_a_id" "$external_pending_id" "pending" "pending" "pending" "pending" 0 "== null"
pass "external outbound pending kept active hold without a journal"

external_unknown="$(request -X POST "${BASE_URL}/api/v1/external/transfers/outbound" \
  -H "Content-Type: application/json" \
  -d "{\"source_account_id\":\"${account_a_id}\",\"destination_institution_code\":\"999001\",\"destination_account_number\":\"9990000001\",\"amount_minor\":8000,\"currency_id\":\"NGN\",\"idempotency_key\":\"uat-external-out-unknown\",\"narration\":\"UAT external outbound unknown\",\"reference\":\"uat-external-out-unknown-ref\",\"scenario\":\"provider_unknown\"}")"
external_unknown_id="$(json_get '.transfer_id' "$external_unknown")"
assert_json "$external_unknown" '.status == "pending" and .provider_status == "provider_unknown" and .ledger_status == "pending" and .reconciliation_status == "manual_review" and .journal_entry_id == null and .hold_id != null' "external outbound provider_unknown mismatch"
assert_balance "$account_a_id" 407000 425000
assert_transfer_hold_status "$external_unknown_id" "active"
assert_transfer_journal_count "$external_unknown_id" 0
assert_reconciliation_item "$external_unknown_id" "provider_unknown" "requery_provider"
assert_history_state "$account_a_id" "$external_unknown_id" "pending" "provider_unknown" "pending" "manual_review" 0 "== null"
pass "external provider_unknown kept hold, avoided fallback, and entered reconciliation"

external_inbound_success="$(request -X POST "${BASE_URL}/api/v1/external/transfers/inbound-events" \
  -H "Content-Type: application/json" \
  -d "{\"provider_event_id\":\"uat-external-in-success-event\",\"provider_reference\":\"uat-external-in-success-ref\",\"destination_account_number\":\"2234567890\",\"amount_minor\":18000,\"currency_id\":\"NGN\",\"status\":\"succeeded\",\"sender_name\":\"UAT Sender\",\"sender_account_number\":\"1234567890\",\"sender_institution_code\":\"999044\",\"narration\":\"UAT external inbound success\"}")"
external_inbound_success_id="$(json_get '.transfer_id' "$external_inbound_success")"
external_inbound_success_journal_id="$(json_get '.journal_entry_id' "$external_inbound_success")"
assert_json "$external_inbound_success" '.status == "succeeded" and .provider_status == "succeeded" and .ledger_status == "posted" and .reconciliation_status == "matched" and .journal_entry_id != null' "external inbound success mismatch"
assert_balance "$account_b_id" 318000 318000
assert_journal_balanced "$external_inbound_success_journal_id" 18000
assert_history_state "$account_b_id" "$external_inbound_success_id" "succeeded" "succeeded" "posted" "matched" 18000 "!= null"
pass "external inbound success credited destination once through a balanced journal"

external_inbound_duplicate="$(request -X POST "${BASE_URL}/api/v1/external/transfers/inbound-events" \
  -H "Content-Type: application/json" \
  -d "{\"provider_event_id\":\"uat-external-in-success-event\",\"provider_reference\":\"uat-external-in-success-ref\",\"destination_account_number\":\"2234567890\",\"amount_minor\":18000,\"currency_id\":\"NGN\",\"status\":\"succeeded\",\"sender_name\":\"UAT Sender\",\"sender_account_number\":\"1234567890\",\"sender_institution_code\":\"999044\",\"narration\":\"UAT external inbound success\"}")"
assert_json "$external_inbound_duplicate" ".transfer_id == \"${external_inbound_success_id}\"" "external inbound duplicate returned a different transfer"
assert_balance "$account_b_id" 318000 318000
assert_transfer_journal_count "$external_inbound_success_id" 1
pass "external inbound duplicate provider event replayed without double-crediting"

external_inbound_conflict_path="${TMP_DIR}/external-inbound-conflict.json"
external_inbound_conflict_code="$(request_status "$external_inbound_conflict_path" -X POST "${BASE_URL}/api/v1/external/transfers/inbound-events" \
  -H "Content-Type: application/json" \
  -d "{\"provider_event_id\":\"uat-external-in-success-event\",\"provider_reference\":\"uat-external-in-success-ref\",\"destination_account_number\":\"2234567890\",\"amount_minor\":19000,\"currency_id\":\"NGN\",\"status\":\"succeeded\",\"sender_name\":\"UAT Sender\",\"sender_account_number\":\"1234567890\",\"sender_institution_code\":\"999044\",\"narration\":\"UAT external inbound conflict\"}")"
[[ "$external_inbound_conflict_code" == "409" ]] || fail "external inbound mismatch expected HTTP 409, got ${external_inbound_conflict_code} body=$(cat "$external_inbound_conflict_path")"
external_inbound_conflict="$(cat "$external_inbound_conflict_path")"
external_inbound_conflict_id="$(json_get '.transfer_id' "$external_inbound_conflict")"
assert_json "$external_inbound_conflict" '.status == "failed" and .provider_status == "succeeded" and .ledger_status == "no_posting" and .reconciliation_status == "manual_review" and .journal_entry_id == null and .message == "provider_event_payload_conflict"' "external inbound mismatch review response mismatch"
assert_transfer_journal_count "$external_inbound_conflict_id" 0
assert_balance "$account_b_id" 318000 318000
assert_reconciliation_item "$external_inbound_conflict_id" "provider_succeeded_ledger_not_posted" "inspect_journal"
pass "external inbound provider-event mismatch created review signal without posting money"

external_inbound_pending="$(request -X POST "${BASE_URL}/api/v1/external/transfers/inbound-events" \
  -H "Content-Type: application/json" \
  -d "{\"provider_event_id\":\"uat-external-in-pending-event\",\"provider_reference\":\"uat-external-in-pending-ref\",\"destination_account_number\":\"2234567890\",\"amount_minor\":7000,\"currency_id\":\"NGN\",\"status\":\"pending\",\"narration\":\"UAT external inbound pending\"}")"
external_inbound_pending_id="$(json_get '.transfer_id' "$external_inbound_pending")"
assert_json "$external_inbound_pending" '.status == "pending" and .provider_status == "pending" and .ledger_status == "pending" and .reconciliation_status == "pending" and .journal_entry_id == null' "external inbound pending mismatch"
assert_transfer_journal_count "$external_inbound_pending_id" 0
assert_balance "$account_b_id" 318000 318000
assert_history_state "$account_b_id" "$external_inbound_pending_id" "pending" "pending" "pending" "pending" 0 "== null"
pass "external inbound pending recorded history without changing balances"

external_inbound_failed="$(request -X POST "${BASE_URL}/api/v1/external/transfers/inbound-events" \
  -H "Content-Type: application/json" \
  -d "{\"provider_event_id\":\"uat-external-in-failed-event\",\"provider_reference\":\"uat-external-in-failed-ref\",\"destination_account_number\":\"2234567890\",\"amount_minor\":8000,\"currency_id\":\"NGN\",\"status\":\"failed\",\"narration\":\"UAT external inbound failed\"}")"
external_inbound_failed_id="$(json_get '.transfer_id' "$external_inbound_failed")"
assert_json "$external_inbound_failed" '.status == "failed" and .provider_status == "failed" and .ledger_status == "no_posting" and .reconciliation_status == "no_action" and .journal_entry_id == null' "external inbound failed mismatch"
assert_transfer_journal_count "$external_inbound_failed_id" 0
assert_balance "$account_b_id" 318000 318000
assert_history_state "$account_b_id" "$external_inbound_failed_id" "failed" "failed" "no_posting" "no_action" 0 "== null"
pass "external inbound failure recorded no-posting without changing balances"

external_inbound_unknown="$(request -X POST "${BASE_URL}/api/v1/external/transfers/inbound-events" \
  -H "Content-Type: application/json" \
  -d "{\"provider_event_id\":\"uat-external-in-unknown-event\",\"provider_reference\":\"uat-external-in-unknown-ref\",\"destination_account_number\":\"0000000023\",\"amount_minor\":9000,\"currency_id\":\"NGN\",\"status\":\"succeeded\",\"narration\":\"UAT external inbound unknown destination\"}")"
external_inbound_unknown_id="$(json_get '.transfer_id' "$external_inbound_unknown")"
assert_json "$external_inbound_unknown" '.status == "failed" and .provider_status == "succeeded" and .ledger_status == "no_posting" and .reconciliation_status == "manual_review" and .journal_entry_id == null and .message == "unknown_destination"' "external inbound unknown destination mismatch"
assert_transfer_journal_count "$external_inbound_unknown_id" 0
assert_balance "$account_b_id" 318000 318000
assert_reconciliation_item "$external_inbound_unknown_id" "provider_succeeded_ledger_not_posted" "inspect_journal"
pass "external inbound unknown destination avoided crediting money and entered reconciliation"

lien="$(request -X POST "${BASE_URL}/api/v1/accounts/${account_b_id}/liens" \
  -H "Content-Type: application/json" \
  -d '{"amount_minor":100000,"currency_id":"NGN","reference":"uat-lien-001","reason":"UAT ops hold"}')"
assert_json "$lien" '.status == "active" and .amount_minor == 100000 and .transfer_id == null and .reference == "uat-lien-001"' "lien placement mismatch"
assert_balance "$account_b_id" 218000 318000
pass "lien reduced available balance only"

pnd="$(request -X POST "${BASE_URL}/api/v1/accounts/${account_b_id}/post-no-debit" \
  -H "Content-Type: application/json" \
  -d '{"reference":"uat-pnd-001","reason":"UAT fraud review"}')"
assert_json "$pnd" '.status == "post_no_debit"' "PND activation mismatch"
assert_blocked_money_request "PND debit should fail" -X POST "${BASE_URL}/api/v1/internal/debits" \
  -H "Content-Type: application/json" \
  -d "{\"account_id\":\"${account_b_id}\",\"amount_minor\":1000,\"currency_id\":\"NGN\",\"idempotency_key\":\"uat-pnd-debit-blocked\",\"narration\":\"blocked\",\"reference\":\"uat-pnd-debit-blocked\"}"
assert_blocked_money_request "PND transfer out should fail" -X POST "${BASE_URL}/api/v1/internal/transfers" \
  -H "Content-Type: application/json" \
  -d "{\"source_account_id\":\"${account_b_id}\",\"destination_account_id\":\"${account_a_id}\",\"amount_minor\":1000,\"currency_id\":\"NGN\",\"idempotency_key\":\"uat-pnd-transfer-out-blocked\",\"narration\":\"blocked\",\"reference\":\"uat-pnd-transfer-out-blocked\"}"
pnd_credit="$(request -X POST "${BASE_URL}/api/v1/internal/credits" \
  -H "Content-Type: application/json" \
  -d "{\"account_id\":\"${account_b_id}\",\"amount_minor\":1000,\"currency_id\":\"NGN\",\"idempotency_key\":\"uat-pnd-credit-001\",\"narration\":\"UAT PND credit\",\"reference\":\"uat-pnd-credit-ref-001\"}")"
assert_json "$pnd_credit" '.status == "succeeded" and .journal_entry_id != null' "PND credit should succeed"
pnd_transfer_in="$(request -X POST "${BASE_URL}/api/v1/internal/transfers" \
  -H "Content-Type: application/json" \
  -d "{\"source_account_id\":\"${account_a_id}\",\"destination_account_id\":\"${account_b_id}\",\"amount_minor\":1000,\"currency_id\":\"NGN\",\"idempotency_key\":\"uat-pnd-transfer-in-001\",\"narration\":\"UAT PND transfer in\",\"reference\":\"uat-pnd-transfer-in-ref-001\"}")"
assert_json "$pnd_transfer_in" '.status == "succeeded" and .journal_entry_id != null' "PND transfer-in should succeed"
assert_balance "$account_a_id" 406000 424000
assert_balance "$account_b_id" 220000 320000
pass "PND blocked outflows and allowed inflows"

assert_blocked_money_request "PND freeze should fail" -X POST "${BASE_URL}/api/v1/accounts/${account_b_id}/freeze" \
  -H "Content-Type: application/json" \
  -d '{"reference":"uat-pnd-freeze-blocked","reason":"UAT freeze over PND should be explicit"}'
pnd_off="$(request -X DELETE "${BASE_URL}/api/v1/accounts/${account_b_id}/post-no-debit" \
  -H "Content-Type: application/json" \
  -d '{"reference":"uat-pnd-off-001","reason":"UAT clear PND before freeze"}')"
assert_json "$pnd_off" '.status == "active"' "PND deactivation mismatch"
pass "PND account could not be frozen without first clearing PND"

frozen="$(request -X POST "${BASE_URL}/api/v1/accounts/${account_b_id}/freeze" \
  -H "Content-Type: application/json" \
  -d '{"reference":"uat-freeze-001","reason":"UAT security review"}')"
assert_json "$frozen" '.status == "frozen"' "freeze mismatch"
assert_blocked_money_request "frozen credit should fail" -X POST "${BASE_URL}/api/v1/internal/credits" \
  -H "Content-Type: application/json" \
  -d "{\"account_id\":\"${account_b_id}\",\"amount_minor\":1000,\"currency_id\":\"NGN\",\"idempotency_key\":\"uat-freeze-credit-blocked\",\"narration\":\"blocked\",\"reference\":\"uat-freeze-credit-blocked\"}"
assert_blocked_money_request "frozen debit should fail" -X POST "${BASE_URL}/api/v1/internal/debits" \
  -H "Content-Type: application/json" \
  -d "{\"account_id\":\"${account_b_id}\",\"amount_minor\":1000,\"currency_id\":\"NGN\",\"idempotency_key\":\"uat-freeze-debit-blocked\",\"narration\":\"blocked\",\"reference\":\"uat-freeze-debit-blocked\"}"
assert_blocked_money_request "frozen transfer in should fail" -X POST "${BASE_URL}/api/v1/internal/transfers" \
  -H "Content-Type: application/json" \
  -d "{\"source_account_id\":\"${account_a_id}\",\"destination_account_id\":\"${account_b_id}\",\"amount_minor\":1000,\"currency_id\":\"NGN\",\"idempotency_key\":\"uat-freeze-transfer-in-blocked\",\"narration\":\"blocked\",\"reference\":\"uat-freeze-transfer-in-blocked\"}"
assert_blocked_money_request "frozen transfer out should fail" -X POST "${BASE_URL}/api/v1/internal/transfers" \
  -H "Content-Type: application/json" \
  -d "{\"source_account_id\":\"${account_b_id}\",\"destination_account_id\":\"${account_a_id}\",\"amount_minor\":1000,\"currency_id\":\"NGN\",\"idempotency_key\":\"uat-freeze-transfer-out-blocked\",\"narration\":\"blocked\",\"reference\":\"uat-freeze-transfer-out-blocked\"}"
assert_no_transfers_for_keys "'uat-pnd-debit-blocked','uat-pnd-transfer-out-blocked','uat-freeze-credit-blocked','uat-freeze-debit-blocked','uat-freeze-transfer-in-blocked','uat-freeze-transfer-out-blocked'"
assert_balance "$account_a_id" 406000 424000
assert_balance "$account_b_id" 220000 320000
pass "freeze blocked all money movement"

echo "Latest audit events:"
sql_exec "SELECT action, entity_type, entity_id, account_id, transfer_id, journal_entry_id, reference, created_at FROM audit_events ORDER BY created_at DESC LIMIT 50;"
assert_audit_action_count "customer.created" 2
assert_audit_action_count "account.created" 2
assert_audit_action_count "internal_credit.posted" 2
assert_audit_action_count "internal_debit.posted" 1
assert_audit_action_count "internal_transfer.posted" 2
assert_audit_action_count "external_outbound.succeeded" 1
assert_audit_action_count "external_outbound.failed" 1
assert_audit_action_count "external_outbound.pending" 1
assert_audit_action_count "external_outbound.provider_unknown" 1
assert_audit_action_count "external_inbound.succeeded" 1
assert_audit_action_count "external_inbound.failed" 1
assert_audit_action_count "external_inbound.pending" 1
assert_audit_action_count "external_inbound.manual_review" 2
assert_audit_action_count "account.lien_placed" 1
assert_audit_action_count "account.pnd_activated" 1
assert_audit_action_count "account.pnd_deactivated" 1
assert_audit_action_count "account.frozen" 1
actor_context_count="$(sql_scalar "SELECT COUNT(*) FROM audit_events WHERE institution_id = '${INSTITUTION_ID}' AND actor_type = 'dev_user' AND actor_id = 'dev-user' AND request_id <> 'service' AND metadata->>'actor_roles' = 'developer'")"
[[ "$actor_context_count" -ge "1" ]] || fail "audit events did not capture authenticated actor context"
pass "audit query contains expected UAT actions"

assert_journal_reconciliation
pass "journal totals and account ledgers reconcile to postings"

echo "UAT simple transaction CBA passed."
