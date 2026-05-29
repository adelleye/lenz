# CBA v0.1 Build 3 - Balance Enquiry Verification

Branch: `goal/cba-v0.1-03-balance-enquiry`
Date: 2026-05-25

## Scope

Implemented:

- Hardened `GET /api/v1/accounts/{account_id}/balance` for real accounts.
- Missing account returns not found.
- Cross-institution account reads return not found.
- Existing account with a missing `account_balances` row returns a controlled data-integrity error.
- Test-only SQL reconciliation now detects missing balance rows, ledger cache mismatches, and available-balance mismatches from active holds.

No new money movement, providers, account controls, audit events, or frontend work was added.

## Behavior Proven

- Newly created accounts return `ledger_minor=0` and `available_minor=0`.
- Active outbound holds reduce `available_minor` without changing `ledger_minor`.
- Released holds stop reducing `available_minor`.
- Consumed holds stop reducing `available_minor` after the ledger posts.
- `account_balances` remains a cache that must reconcile to postings plus active holds.
- The handler response is JSON and validates against the OpenAPI response schema.
- Raw internal balance errors remain sanitized.

## Files Changed

- `apps/core/internal/corebanking/model.go`
- `apps/core/internal/corebanking/service.go`
- `apps/core/internal/corebanking/sql_account_repository.go`
- `apps/core/internal/corebanking/service_test.go`
- `apps/core/internal/corebanking/handler_test.go`
- `apps/core/internal/corebanking/sql_repository_integration_test.go`

## Verification Commands

```sh
go generate ./apps/core/internal/corebanking
git check-ignore -v apps/core/internal/corebanking/corebanking.gen.go
go test -count=1 ./apps/core/internal/corebanking
go test -race -count=1 ./apps/core/internal/corebanking
go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
go build ./apps/core/... ./apps/auth/... ./packages/shared/...
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/demo_transfer_spine.sh
LENZ_INTEGRATION_DATABASE_URL=postgres://lenzcore:lenzcore123@localhost:55432/lenzcore?sslmode=disable go test -count=1 -tags=integration ./apps/core/internal/corebanking -run 'TestSQLRepository(BalanceEnquiryIntegration|TransferSpineIntegrationConcurrentReplay)'
```

All commands passed.

Generated code remains ignored:

```text
.gitignore:13:apps/core/internal/corebanking/corebanking.gen.go apps/core/internal/corebanking/corebanking.gen.go
```

## Manual API And DB Evidence

Manual local proof used Docker/Postgres on `localhost:55432` and API port `3002`.

Flow:

1. Seed demo institution and branch.
2. Create customer through `POST /api/v1/customers`.
3. Create account through `POST /api/v1/accounts`.
4. Read balance through `GET /api/v1/accounts/{account_id}/balance`.
5. Query `account_balances` directly.
6. Compare API and DB balance values.

Evidence:

```text
manual_customer_id=cd81c292-1b92-480a-9c41-aab6ea4213d4
manual_account_id=18538066-b710-4c88-bff7-9f3ecfba6d9f
manual_account_number=1211255426
manual_api_balance=18538066-b710-4c88-bff7-9f3ecfba6d9f 0 0 NGN null
manual_db_balance=18538066-b710-4c88-bff7-9f3ecfba6d9f 0 0 NGN null
```

## New Test Evidence

- `TestBalanceEnquiryRejectsMissingCrossTenantAndInvalidAccount`
- `TestBalanceEnquiryTracksActiveReleasedAndConsumedHolds`
- `TestGetAccountBalanceRouteReturnsNewAccountZeroBalanceAndMatchesSchema`
- `TestGetAccountBalanceRouteDeniesCrossInstitutionRead`
- `TestGetAccountBalanceRouteRequiresAuth`
- `TestSQLRepositoryBalanceEnquiryIntegration`

## Deferred

- Internal credits/debits.
- Account controls such as lien, freeze, and post-no-debit.
- Production reconciliation jobs.
- Audit-event writing.
- Real provider/NIBSS behavior.
