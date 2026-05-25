# CBA v0.1 Build 4 - Internal Credit Verification

Branch: `goal/cba-v0.1-04-internal-credit`
Date: 2026-05-25

## Scope

Implemented:

- `POST /api/v1/internal/credits`
- Credits an active customer account from a safe internal source account.
- Uses the existing corebanking module pattern: OpenAPI, strict handler, service, repository, SQL transaction.
- Reuses the existing `RecordTransfer` ledger path instead of adding a bespoke balance writer.
- Creates one succeeded inbound transfer with provider `ledger_internal`.
- Posts one balanced journal: debit internal source account, credit customer account.
- Updates customer and internal source `ledger_minor` and `available_minor`.
- Shows the credit in account transaction history.
- Rejects invalid amount, unsupported currency, missing idempotency key, cross-institution access, inactive customer accounts, and unsafe source accounts.

No debit, internal transfer, fees, loans, provider/NIBSS rail, audit-event writing, frontend, or full chart of accounts was added.

## Behavior Proven

- Internal credit increases customer ledger and available balance.
- The default source account must be an active internal debit-normal account with `allow_negative_balance=true`.
- Duplicate idempotency returns the original transfer and does not double-credit.
- Ten concurrent requests with the same idempotency key create one transfer, one journal, and two postings.
- Raw internal errors remain sanitized behind `internal_server_error` plus `request_id`.
- Generated OpenAPI files remain ignored.

## Files Changed

- `design/openapi/core/corebanking.yaml`
- `apps/core/internal/corebanking/model.go`
- `apps/core/internal/corebanking/repository.go`
- `apps/core/internal/corebanking/sql_account_repository.go`
- `apps/core/internal/corebanking/service.go`
- `apps/core/internal/corebanking/handler.go`
- `apps/core/internal/corebanking/service_test.go`
- `apps/core/internal/corebanking/handler_test.go`
- `apps/core/internal/corebanking/sql_repository_integration_test.go`

## Verification Commands

```sh
go generate ./apps/core/internal/corebanking
go generate ./apps/core/internal/institution
git check-ignore -v apps/core/internal/corebanking/corebanking.gen.go apps/core/internal/institution/institution.gen.go
go test -race -count=1 ./apps/core/internal/corebanking
go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
go build ./apps/core/... ./apps/auth/... ./packages/shared/...
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/demo_transfer_spine.sh
LENZ_INTEGRATION_DATABASE_URL=postgres://lenzcore:lenzcore123@localhost:55432/lenzcore?sslmode=disable go test -count=1 -tags=integration ./apps/core/internal/corebanking -run 'TestSQLRepositoryInternalCredit(Integration|ConcurrentIdempotency)' -v
git diff --check
```

All commands passed.

Generated code remains ignored:

```text
.gitignore:13:apps/core/internal/corebanking/corebanking.gen.go apps/core/internal/corebanking/corebanking.gen.go
.gitignore:14:apps/core/internal/institution/institution.gen.go apps/core/internal/institution/institution.gen.go
```

## Manual API And DB Evidence

Manual local proof used Docker/Postgres on `localhost:55432` and API port `3011`.

Flow:

1. Seed demo institution and internal source account.
2. Create customer through `POST /api/v1/customers`.
3. Create customer account through `POST /api/v1/accounts`.
4. Credit the new account through `POST /api/v1/internal/credits`.
5. Replay the same internal-credit request.
6. Read balance and transaction history through HTTP.
7. Query `transfers`, `journal_entries`, `postings`, and `account_balances` directly.

Evidence:

```text
manual_http_status=PASS
customer_id=fd7becda-246e-4909-90c0-6de8142d282f
account_id=2a07ece3-f306-47ba-8d3c-c13e44f460da
transfer_id=81e15add-b208-4a93-afe3-1c6d9bf3d826
duplicate_transfer_id=81e15add-b208-4a93-afe3-1c6d9bf3d826
journal_id=97ee0563-951f-4cf7-935d-931f754ea9d3
balance=12345:12345
transfer_count_for_idempotency=1
journal_count_for_transfer=1
postings=
credit:12345
debit:12345
```

## New Test Evidence

- `TestInternalCreditPostsBalancedLedgerAndHistory`
- `TestInternalCreditIdempotencyDoesNotDoubleCredit`
- `TestInternalCreditRejectsInvalidInput`
- `TestCreateInternalCreditRouteCreditsBalanceAndHistory`
- `TestCreateInternalCreditRouteRequiresAuth`
- `TestCreateInternalCreditRouteRejectsMismatchedInstitutionHeader`
- `TestCreateInternalCreditRouteRejectsInvalidRequestBody`
- `TestInternalCreditInternalErrorsAreSanitized`
- `TestSQLRepositoryInternalCreditIntegration`
- `TestSQLRepositoryInternalCreditConcurrentIdempotency`

## Deferred

- Internal debit.
- Internal account-to-account transfer.
- Account controls such as freeze, lien, and post-no-debit.
- Audit-event writing for money actions.
- Maker-checker/RBAC.
- Real provider/NIBSS behavior.
