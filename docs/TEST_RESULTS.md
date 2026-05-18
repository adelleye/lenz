# Lenz Core Transaction Spine Test Results

Run window: `2026-05-18 08:57:08-09:16 WAT +0100`

Commit under test: `3991e3f0dd6899ab64ba269337184c24f7d207b1`

Branch: `main`

Raw evidence directory: `/tmp/lenz-core-e2e-20260518T085628`

Worktree note: this run started with a dirty worktree:

```text
 M apps/core/internal/corebanking/service_test.go
 M apps/core/internal/corebanking/sql_errors.go
 M apps/core/internal/corebanking/sql_provider_event_repository.go
 M apps/core/internal/corebanking/sql_store_integration_test.go
 M apps/core/internal/corebanking/sql_transfer_repository.go
 M design/openapi/core/corebanking.yaml
?? docs/TEST_PLAN.md
?? docs/TEST_RESULTS.md
```

I did not change money logic during this verification pass. The only intended
repo edits from this run are the two docs. `go generate` recreated ignored
generated files locally.

Note on raw logs: `extended-http-sql.log` is an abandoned harness attempt where
the shell helper mixed log text into captured HTTP status codes and blocked on
the API process. It is retained for traceability but is not used for pass/fail
judgement. Use `extended-http-sql-rerun.log`, `demo-off-final.log`,
`prod-demo-mode-final.log`, `db-down-final.log`, and
`race-overspend-reversal.log` for the final operator probes.

## Judgement

Safe to hand off to the backend team as a verified demo/prototype transaction
spine.

Not safe to hand off as a production banking core. The money-spine demo,
Postgres migrations, generated strict handler path, ledger balancing,
idempotency, holds, reversals, auth gates, tenant checks, and concurrency probes
passed. Production gaps remain around real auth/RBAC, maker-checker, real
provider signatures/webhooks, audit immutability, limits, fraud monitoring,
reconciliation, statements, and regulatory reporting.

The smallest issues found in this run are:

1. Empty transaction lists serialize as `null` instead of `[]`.
2. `audit_events` exists but is not written by money actions.
3. `packages/shared/httpmiddleware/oapi_validate.go` contains unused middleware
   functions.

## Required Commands

| Command | Result | Evidence |
|---|---:|---|
| `go generate ./apps/core/internal/corebanking` | PASS | exit 0, no output |
| `go generate ./apps/core/internal/institution` | PASS | exit 0, no output |
| `git check-ignore -v apps/core/internal/corebanking/corebanking.gen.go` | PASS | `.gitignore:13:apps/core/internal/corebanking/corebanking.gen.go` |
| `git check-ignore -v apps/core/internal/institution/institution.gen.go` | PASS | `.gitignore:14:apps/core/internal/institution/institution.gen.go` |
| `go test -race ./apps/core/internal/corebanking` | PASS | exact command passed from cache; uncached repeat also passed |
| `go test ./apps/core/... ./apps/auth/... ./packages/shared/...` | PASS | exact command passed from cache; uncached repeat also passed |
| `go build ./apps/core/... ./apps/auth/... ./packages/shared/...` | PASS | exit 0, no output |
| `scripts/demo_transfer_spine.sh` | PASS | ended with `DEMO TRANSFER SPINE: PASS` |
| Docker/Postgres migration from clean DB | PASS | demo script reset compose volumes and applied all migrations |

Uncached repeats:

```text
$ go test -race -count=1 ./apps/core/internal/corebanking
ok  	lenz-core/apps/core/internal/corebanking	1.906s

$ go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
?   	lenz-core/apps/core	[no test files]
?   	lenz-core/apps/core/cmd/migrate	[no test files]
ok  	lenz-core/apps/core/internal/corebanking	0.673s
?   	lenz-core/apps/core/internal/institution	[no test files]
ok  	lenz-core/apps/core/server	1.360s
?   	lenz-core/apps/auth	[no test files]
ok  	lenz-core/apps/auth/authn	1.928s
?   	lenz-core/apps/auth/authz	[no test files]
?   	lenz-core/packages/shared/config	[no test files]
?   	lenz-core/packages/shared/httpmiddleware	[no test files]
?   	lenz-core/packages/shared/utils	[no test files]
```

Migration rerun against the same database:

```text
$ DATABASE_URL='postgres://lenzcore:lenzcore123@localhost:55432/lenzcore?sslmode=disable' go run ./apps/core/cmd/migrate
skip 20260516000100_transaction_spine
skip 20260516000200_account_balance_policies
skip 20260516000300_provider_unknown_status
```

Clean-clone check against committed `HEAD`:

```text
$ git clone --no-hardlinks . /tmp/lenz-core-clean-clone-20260518T0916
$ go generate ./apps/core/internal/corebanking
$ go generate ./apps/core/internal/institution
$ go test ./apps/core/... ./apps/auth/... ./packages/shared/...
ok  	lenz-core/apps/core/internal/corebanking	0.931s
ok  	lenz-core/apps/core/server	1.004s
ok  	lenz-core/apps/auth/authn	1.482s
```

The clean clone proves committed `HEAD` can regenerate and test. It does not
include the current dirty worktree changes.

## Key Evidence

Clean Docker/Postgres/demo flow:

```text
Resetting Docker Compose services and volumes for a clean demo database...
PASS: Docker Compose started healthy Postgres and Redis
Running migrations...
applied 20260516000100_transaction_spine
applied 20260516000200_account_balance_policies
applied 20260516000300_provider_unknown_status
PASS: SQL integration test suite passed
PASS: API started successfully
PASS: mock inbound credited the account and posted a balanced journal
PASS: duplicate idempotency key did not double-credit
PASS: duplicate provider_event_id did not double-credit
PASS: mock outbound debited the account
PASS: pending outbound created a hold and reduced available balance only
PASS: failed pending outbound released its hold without moving ledger money
PASS: successful pending outbound posted ledger entries and consumed its hold
PASS: reversal created a new transfer, balanced journal entry, and manual-review deficit
PASS: transaction history came from Lenz transfer/journal/posting records
PASS: admin transfer list returned all demo transfers
DEMO TRANSFER SPINE: PASS
```

Auth, CORS, validation, and tenant probes:

```text
health_no_auth status=200 body={"status": "ok"}
balance_no_auth status=401 body={"message":"unauthorized"}
balance_wrong_bearer status=401 body={"message":"unauthorized"}
query_string_token_rejected status=401 body={"message":"unauthorized"}
principal_scope_without_header status=200
mismatched_institution_header status=403 body={"message":"forbidden"}
cors_safe_dev_allow_origin got=http://localhost:5173
cors_untrusted_allow_origin got=<missing>
invalid_uuid_path status=400 body={"message":"invalid_request"}
negative_amount status=400 body={"message":"invalid_request"}
zero_amount status=400 body={"message":"invalid_request"}
missing_idempotency_key status=400 body={"message":"invalid_request"}
unknown_status status=400 body={"message":"invalid_request"}
unknown_scenario status=400 body={"message":"invalid_request"}
oversized_body status=413 body=Request Entity Too Large
tenant_a_read_b_balance status=404 body={"message":"not_found"}
tenant_a_list_b_transactions status=200 body=null
tenant_a_get_b_journal status=404 body={"message":"not_found"}
tenant_a_reverse_b_transfer status=404 body={"message":"not_found"}
```

Transaction history pagination:

```text
PASS: pagination_seeded_205_inbound_rows got=205
PASS: history_default_length got=100
PASS: history_max_length got=200
PASS: history_cursor_duplicates got=0
PASS: history_cursor_rows_not_before got=0
```

Concurrent replay and race probes:

```text
concurrent_provider_event ... unique_ids=1 balance_delta=3333
PASS: concurrent_provider_event_200_count got=10
PASS: concurrent_provider_event_unique_ids got=1
PASS: concurrent_provider_event_balance_delta got=3333

concurrent_inbound_idempotency ... unique_ids=1 balance_delta=2222
PASS: concurrent_inbound_idempotency_200_count got=10
PASS: concurrent_inbound_idempotency_unique_ids got=1
PASS: concurrent_inbound_idempotency_balance_delta got=2222

concurrent_outbound_idempotency ... unique_ids=1 balance_delta=-1111
PASS: concurrent_outbound_idempotency_200_count got=10
PASS: concurrent_outbound_idempotency_unique_ids got=1
PASS: concurrent_outbound_idempotency_balance_delta got=-1111

concurrent_pending_settlement ... unique_ids=1 expected_id_count=10 balance_delta=-777 transfer_count=1 journal_count=1
PASS: concurrent_pending_settlement_200_count got=10
PASS: concurrent_pending_settlement_unique_ids got=1
PASS: concurrent_pending_settlement_transfer_count got=1
PASS: concurrent_pending_settlement_journal_count got=1

concurrent_overspend before=32410 after=12410 delta=-20000 succeeded=1 failed=1 codes=1 200200
outbound_reversal_race outbound_code=200
outbound_reversal_race reversal_code=200
race_sql_journal_mismatches=0 race_sql_balance_mismatches=0
```

SQL reconciliation and indexes:

```text
sql_journal_mismatches=0
sql_balance_mismatches=0
sql_required_indexes="postings_account_idx,provider_events_institution_id_provider_provider_event_id_key,transfers_account_idx,transfers_institution_created_idx,transfers_institution_id_idempotency_key_key,transfers_pending_provider_reference_idx"
sql_required_index_count=6
audit_events_count=0
```

Demo-mode and sanitized internal-error checks:

```text
demo_off_seed_status=404 body={"message":"not_found"}
demo_off_mock_mutation_status=404 body={"message":"not_found"}
demo_off_balance_before=7410 after=7410 delta=0

production_demo_mode_exit=1 log=2026/05/18 09:15:30 LENZ_DEMO_MODE=true is not allowed when APP_ENV/ENV is production

forced_db_down_status=500 body={"message":"internal_server_error","request_id":"operator-forced-db-down"}
forced_db_down_server_log=2026/05/18 09:15:50 internal_error request_id=operator-forced-db-down method=GET path=/api/v1/accounts/44444444-4444-4444-4444-444444444444/balance error=dial tcp [::1]:55432: connect: connection refused
postgres_status_after_db_down=healthy
```

HybridProvider and service-level safety tests:

```text
--- PASS: TestHybridProviderPrimaryTransferSuccess
--- PASS: TestHybridProviderNameEnquiryFallsBackWhenPrimaryFails
--- PASS: TestHybridProviderTransferDoesNotFallbackOnTimeoutOrUnknownStatus
--- PASS: TestHybridProviderTransferFallbackRequiresPreSubmissionFailure
--- PASS: TestHybridProviderRequeryUsesOriginalTransferProvider
--- PASS: TestUnsupportedProviderWebhookRejectedBeforeMoneyMovement
--- PASS: TestTenantScopingPreventsCrossTenantReads
--- PASS: TestInternalErrorsAreSanitized
--- PASS: TestTransactionHistoryDefaultsToOneHundredAndOrdersNewestFirst
--- PASS: TestTransactionHistoryCapsLimitAndPaginatesBeforeCreatedAt
```

Unused-code sweep:

```text
$ go vet ./apps/core/... ./apps/auth/... ./packages/shared/...
# no output, exit 0

$ go run honnef.co/go/tools/cmd/staticcheck@latest -checks=U1000 ./apps/core/... ./apps/auth/... ./packages/shared/...
packages/shared/httpmiddleware/oapi_validate.go:21:6: func requestMiddleware is unused (U1000)
packages/shared/httpmiddleware/oapi_validate.go:81:6: func returnMiddleware is unused (U1000)
exit status 1

$ rg -n "audit_events" . -g '!**/*.gen.go'
migrations/20260516000100_transaction_spine.up.sql:161:CREATE TABLE IF NOT EXISTS audit_events (
migrations/20260516000100_transaction_spine.down.sql:1:DROP TABLE IF EXISTS audit_events;
apps/core/internal/corebanking/sql_store_integration_test.go:604:	audit_events,
```

Full `staticcheck` also reported generated-file style warnings in
`apps/core/internal/corebanking/corebanking.gen.go`; those are not cleanup
candidates because generated files must not be hand-edited.

## Scenario Matrix

| ID | Scenario | Status | Evidence |
|---:|---|---|---|
| 1 | Generated files ignored if repo convention | PASS | `git check-ignore` matched `.gitignore` lines 13 and 14 |
| 2 | `go generate` recreates generated code | PASS | both generate commands exit 0 |
| 3 | `HTTPServer` implements `StrictServerInterface` | PASS | compile assertion in `handler.go`; build/tests pass |
| 4 | Strict handler path used | PASS | `NewStrictHandlerWithOptions(...)` in `handler.go` |
| 5 | Invalid OpenAPI request shapes rejected before service | PASS | invalid UUID 400; invalid mock outbound service-call test passed |
| 6 | Clean Docker Postgres starts | PASS | demo script reset volumes and healthy Postgres |
| 7 | Migrations apply from scratch | PASS | three migrations applied in clean demo flow |
| 8 | API starts | PASS | demo and operator probe APIs reached health |
| 9 | Health works without auth | PASS | `GET /api/v1/health` 200 |
| 10 | Non-health endpoints require auth | PASS | no token and wrong token returned 401 |
| 11 | Demo seed creates institution, branch, customer, account, clearing | PASS | demo seed response and account lookup |
| 12 | Initial balance correct | PASS | demo seed account started from 0 in clean flow |
| 13 | Successful inbound credits customer | PASS | demo inbound and journal balance checks |
| 14 | Successful outbound debits customer | PASS | demo outbound balance decreased |
| 15 | History shows inbound/outbound entries | PASS | demo history assertions |
| 16 | Admin transfer list shows transfers | PASS | demo admin list returned all demo transfers |
| 17 | Admin journal endpoint shows balanced postings | PASS | demo `assert_journal_balanced` uses admin journal endpoint |
| 18 | Every posted transfer has balanced journal | PASS | SQL `sql_journal_mismatches=0` |
| 19 | Journal postings sum to journal totals | PASS | SQL debit/credit sums matched journal totals |
| 20 | Stored ledger balance matches reconstructed postings | PASS | SQL `sql_balance_mismatches=0` |
| 21 | Available equals ledger minus active holds | PASS | SQL balance/hold reconciliation 0 |
| 22 | Corrections are new events, not mutations | PASS | reversal creates new transfer/journal; original remains by test |
| 23 | History includes succeeded, pending, failed, reversal rows | PASS | demo history assertions |
| 24 | History comes from Lenz records, not provider memory | PASS | focused transaction-history test and demo |
| 25 | History newest-first | PASS | focused history ordering test |
| 26 | History pagination default/max/cursor/no duplicates | PASS | HTTP default 100, cap 200, cursor duplicates 0 |
| 27 | Large history request bounded | PASS | `limit=500` returned 200 rows |
| 28 | Pending outbound creates hold | PASS | demo pending outbound |
| 29 | Pending outbound reduces available not ledger | PASS | demo available/ledger pair |
| 30 | Failed pending outbound releases hold | PASS | demo failed settlement |
| 31 | Successful pending outbound consumes hold/posts ledger | PASS | demo settlement and journal |
| 32 | Pending inbound appears but does not increase balance | PASS | demo pending inbound |
| 33 | Successful inbound posts ledger/increases available | PASS | demo inbound |
| 34 | Same inbound idempotency key does not double-credit | PASS | demo duplicate and HTTP concurrent idempotency |
| 35 | Same outbound idempotency key does not double-debit | PASS | HTTP concurrent outbound idempotency |
| 36 | Same provider event does not double-credit | PASS | demo duplicate provider event |
| 37 | Same provider event replayed concurrently does not double-credit | PASS | HTTP and SQL concurrent replay passed |
| 38 | Same idempotency key sent concurrently does not double-post | PASS | HTTP and SQL concurrent replay passed |
| 39 | Standard account cannot send more than available | PASS | insufficient funds demo and overspend race |
| 40 | Standard account cannot casually go negative | PASS | insufficient spend rejected |
| 41 | Reversal deficit marked manual_review/reversal_deficit | PASS | demo reversal deficit assertion |
| 42 | Reversal deficit not spendable | PASS | focused service test |
| 43 | Further outbound after reversal deficit rejected | PASS | focused service test |
| 44 | Overdraft-capable account represented separately | PASS | focused service test |
| 45 | Overdraft limits not implemented | DOCUMENTED GAP | representation only; no limits engine |
| 46 | Reversal creates new transfer and journal | PASS | demo and focused reversal tests |
| 47 | Original transfer remains unchanged | PASS | focused reversal tests |
| 48 | Reversal on sufficient balance posts normally | PASS | focused reversal test |
| 49 | Reversal on insufficient balance creates manual-review deficit | PASS | demo and focused test |
| 50 | Reversal cannot be performed across institutions | PASS | tenant A reversing tenant B transfer returned 404 |
| 51 | Duplicate reversal does not double-reverse | PASS | focused idempotency/reversal test |
| 52 | Primary provider success does not call fallback | PASS | HybridProvider unit test |
| 53 | Name enquiry/read-only failure can use fallback | PASS | HybridProvider unit test |
| 54 | Transfer timeout/unknown does not fallback | PASS | HybridProvider unit test |
| 55 | Transfer may fallback only on definite pre-submission failure | PASS | HybridProvider unit test |
| 56 | Requery uses original provider | PASS | HybridProvider unit test |
| 57 | Wrong provider/reference webhook rejected/manual_review | PARTIAL | service rejects unsupported provider before money movement; no real webhook endpoint/signature path exists |
| 58 | Provider unknown status does not duplicate money | PASS | focused provider-unknown test |
| 59 | Institution A cannot read B balance | PASS | HTTP 404 for B account |
| 60 | Institution A cannot list B transactions | PASS WITH ISSUE | no rows leaked, but body was `null` instead of `[]` |
| 61 | Institution A cannot inspect B journal | PASS | HTTP 404 |
| 62 | Institution A cannot reverse B transfer | PASS | HTTP 404 |
| 63 | Mismatched `X-Institution-ID` rejected | PASS | HTTP 403 |
| 64 | Missing principal/institution context rejected | PASS | no token 401 |
| 65 | Caller cannot choose tenant by header | PASS | principal-derived scope test and HTTP mismatch 403 |
| 66 | No token rejected on non-health routes | PASS | HTTP 401 |
| 67 | Wrong bearer token rejected | PASS | HTTP 401 |
| 68 | Query-string `access_token` rejected | PASS | HTTP 401 |
| 69 | CORS rejects untrusted origins | PASS | no `Access-Control-Allow-Origin` |
| 70 | CORS allows configured safe dev origins | PASS | `http://localhost:5173` allowed |
| 71 | Demo routes disabled by default | PASS | demo-off routes returned 404 |
| 72 | Demo routes work only when enabled in development | PASS | enabled seed 200; disabled 404 |
| 73 | Demo mode true in production fails startup | PASS | process exited 1 with explicit guard log |
| 74 | Demo/mock routes cannot mutate ledger when off | PASS | demo-off mutation 404, balance delta 0 |
| 75 | Unexpected internal errors generic with request ID | PASS | forced DB-down returned generic 500 with request ID |
| 76 | Raw DB/provider/internal strings not exposed | PASS | client body did not include DB error |
| 77 | Detailed errors logged server-side only | PASS | server log contained connection-refused detail |
| 78 | Invalid UUID rejected | PASS | HTTP 400 |
| 79 | Negative amount rejected | PASS | HTTP 400 |
| 80 | Zero amount rejected | PASS | HTTP 400 |
| 81 | Missing idempotency key rejected | PASS | HTTP 400 |
| 82 | Unknown provider status/scenario rejected | PASS | HTTP 400 |
| 83 | Invalid/missing institution context rejected | PASS | 401/403 probes |
| 84 | Oversized request body rejected | PASS | HTTP 413 |
| 85 | Two simultaneous outbounds cannot overspend | PASS | one succeeded, one failed, balance delta one debit |
| 86 | Outbound and reversal racing leaves valid ledger | PASS | both calls completed and SQL mismatches stayed 0 |
| 87 | Concurrent provider settlement/requery no double-post | PASS | HTTP and SQL pending-settlement replay |
| 88 | Race detector for corebanking tests | PASS | `go test -race` passed |
| 89 | Fresh database migration works | PASS | clean Docker demo migrations applied |
| 90 | Migration runner safe repeatedly | PASS | rerun reported `skip` |
| 91 | Pending provider-reference settlement index exists | PASS | `transfers_pending_provider_reference_idx` |
| 92 | Institution transfer listing by created_at index exists | PASS | `transfers_institution_created_idx` |
| 93 | Unique constraints for idempotency/provider events | PASS | transfer and provider-event unique indexes |
| 94 | Transaction history account/date index exists | PASS | `transfers_account_idx`, `postings_account_idx` |
| 95 | Clean clone can regenerate and test | PASS WITH NOTE | committed `HEAD` clone passed; dirty worktree not included |
| 96 | Every money action leaves audit/event trail | PARTIAL | transfers/journals/postings exist; `audit_events_count=0` |
| 97 | Transfer traceability fields exist | PASS | institution/account/provider/reference/status/timestamps present |
| 98 | Manual-review reversal deficits visible to ops/admin paths | PASS | demo reversal deficit and admin transfer list |
| 99 | Maker-checker gaps documented | DOCUMENTED GAP | not implemented |
| 100 | Real CBN/MFB production gaps documented | DOCUMENTED GAP | real auth/RBAC, KYC/BVN, limits, fraud, reconciliation, statements, regulatory returns, provider signatures, audit immutability |

## Failed Or Issue Scenarios

### F1: Empty transaction list returns `null`, not `[]`

Related scenario: 60.

Evidence:

```text
tenant_a_list_b_transactions status=200 body=null
PASS: tenant_a_list_b_transactions_length got=0
FAIL: tenant_a_list_b_transactions_empty_array body=null
```

Risk: no tenant data leaked, so this is not a tenant-isolation failure. It is an
API contract and client ergonomics issue because the OpenAPI response is an
array and consumers normally expect an empty list, not JSON `null`.

Smallest safe fix: initialize transaction slices to empty before returning from
the SQL repository or normalize nil slices in the handler response path.

### F2: `audit_events` table is unused

Related scenario: 96.

Evidence:

```text
audit_events_count=0
rg -n "audit_events" . -g '!**/*.gen.go'
migrations/20260516000100_transaction_spine.up.sql:161:CREATE TABLE IF NOT EXISTS audit_events (
migrations/20260516000100_transaction_spine.down.sql:1:DROP TABLE IF EXISTS audit_events;
apps/core/internal/corebanking/sql_store_integration_test.go:604:	audit_events,
```

Risk: the ledger event trail is strong for money reconstruction, but there is
not yet a separate compliance-shaped audit trail for actor/action metadata.

Smallest safe fix: define the audit event contract, then write audit records in
the same database transaction as money actions. Keep it small: start with
transfer created, pending hold created/released/consumed, reversal created, and
manual-review deficit created.

### F3: Unused OpenAPI validation middleware helpers

Related cleanup request: unused-code sweep.

Evidence:

```text
packages/shared/httpmiddleware/oapi_validate.go:21:6: func requestMiddleware is unused (U1000)
packages/shared/httpmiddleware/oapi_validate.go:81:6: func returnMiddleware is unused (U1000)
```

Cross-check: `rg` found no references outside the file. `go vet` was clean.

Risk: low runtime risk because the functions are not called. The maintenance
risk is confusion: there are unused libopenapi validation helpers beside the
currently used generated strict-handler validation path.

Smallest safe fix: in a cleanup-only PR, either remove
`packages/shared/httpmiddleware/oapi_validate.go` or wire it intentionally after
deciding the repo wants libopenapi request validation in addition to
oapi-codegen strict request binding.

## Untested Or Intentionally Out Of Scope

- No real provider was connected.
- No real provider webhook signature verification exists or was tested.
- `HybridProvider` is covered by unit tests but is not wired into the running
  API server; the API currently uses `MockNIPProvider`.
- No frontend, customer UI, statements, limits engine, maker-checker, KYC/BVN,
  regulatory returns, fraud monitoring, or production RBAC was tested.
- No long-running load test was run beyond targeted race/concurrency probes.
- The clean-clone check used committed `HEAD`; it did not include the dirty
  worktree changes present at the start of this run.

## Recommended Next Fixes

1. Return `[]` instead of `null` for empty transaction-history responses.
2. Decide and implement the minimal audit-event write path for money actions.
3. Remove or intentionally wire the unused
   `packages/shared/httpmiddleware/oapi_validate.go` helpers.
4. Keep production gaps visible until real auth/RBAC, maker-checker, KYC/BVN,
   limits, fraud monitoring, reconciliation, statements, regulatory returns,
   provider signatures, and audit immutability are designed.
