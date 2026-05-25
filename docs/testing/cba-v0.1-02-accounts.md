# CBA v0.1 Build 2 - Accounts Verification

Branch: `goal/cba-v0.1-02-accounts`

## Scope

Implemented:

- `POST /api/v1/accounts`
- `GET /api/v1/accounts/{account_id}`
- Existing `GET /api/v1/customers/{customer_id}/accounts` now returns `[]` for empty lists.

Account creation is limited to customer deposit accounts:

- `kind=customer`
- `product_type=standard_wallet|standard_savings|standard_current`
- `allow_negative_balance=false`
- `normal_balance=credit`
- `status=active`
- `currency_id=NGN`
- starting `ledger_minor=0`
- starting `available_minor=0`

No money movement, providers, overdrafts, fees, statements, controls, or frontend work were added.

## Product Context Checked

- CBN NUBAN standard: 10-digit account number with a check digit derived from bank code plus 9-digit serial number.
- BankOne account API docs: account APIs include creation/enquiry plus later controls such as freeze, lien, PND, statements, and transactions.

Decision: this build validates a supplied 10-digit `account_number`. Full NUBAN generation/check-digit validation is deferred until institution bank-code setup is available.

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

No migration was needed. Existing `accounts` and `account_balances` tables already support this build.

## Verification Commands

```sh
go generate ./apps/core/internal/corebanking
go generate ./apps/core/internal/institution
git check-ignore -v apps/core/internal/corebanking/corebanking.gen.go
git check-ignore -v apps/core/internal/institution/institution.gen.go
go test -race -count=1 ./apps/core/internal/corebanking
go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
go build ./apps/core/... ./apps/auth/... ./packages/shared/...
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/demo_transfer_spine.sh
LENZ_INTEGRATION_DATABASE_URL=postgres://lenzcore:lenzcore123@localhost:55432/lenzcore?sslmode=disable go test -count=1 -tags=integration ./apps/core/internal/corebanking -run 'TestSQLRepository(AccountCreateGetListIntegration|TransferSpineIntegrationConcurrentReplay)'
```

All commands passed.

Generated code remains ignored:

```text
.gitignore:13:apps/core/internal/corebanking/corebanking.gen.go apps/core/internal/corebanking/corebanking.gen.go
.gitignore:14:apps/core/internal/institution/institution.gen.go apps/core/internal/institution/institution.gen.go
```

## Manual API And DB Evidence

Manual API flow:

1. Seed demo institution/branch.
2. Create a customer through `POST /api/v1/customers`.
3. Create an account for that customer through `POST /api/v1/accounts`.
4. Read it through `GET /api/v1/accounts/{account_id}`.
5. List it through `GET /api/v1/customers/{customer_id}/accounts`.
6. Try mismatched `X-Institution-ID`; request returned `403`.
7. Query Postgres account and balance rows.

Evidence:

```text
manual_customer_id=69ae68bc-59cb-4770-9526-89f6340a587e
manual_account_response={"id":"8a7e2939-06f3-4663-ac0f-b3dbd29a25e4","institution_id":"11111111-1111-1111-1111-111111111111","customer_id":"69ae68bc-59cb-4770-9526-89f6340a587e","account_number":"1234567891","product_type":"standard_wallet","allow_negative_balance":false,"currency_id":"NGN","normal_balance":"credit","status":"active"}
manual_get_account_status=200
manual_customer_accounts=[{"id":"8a7e2939-06f3-4663-ac0f-b3dbd29a25e4","account_number":"1234567891"}]
manual_cross_institution_status=403
manual_db_account_row=1234567891|standard_wallet|false|credit|active
manual_db_balance_row=0|0|NGN|null
```

## Deferred

- Full NUBAN generation/check-digit validation.
- Account controls such as freeze, lien, and post-no-debit.
- Internal GL account creation.
- Loans, overdrafts, fees, statements, and providers.
