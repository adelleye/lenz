# CBA v0.1 Build 05: Internal Debit Proof

## Scope

Implemented `POST /api/v1/internal/debits` as production-shaped internal ledger money-out:

- debits an active customer account
- credits the configured safe internal settlement account
- writes one balanced journal with two postings
- updates `ledger_minor` and `available_minor`
- returns the existing transfer for duplicate idempotency keys
- rejects insufficient funds without creating a transfer, journal, or postings

This is not a full production CBA. It is the next real transaction-spine slice after customers, accounts, balances, and internal credits.

## Files Changed

- `design/openapi/core/corebanking.yaml`
- `apps/core/internal/corebanking/handler.go`
- `apps/core/internal/corebanking/model.go`
- `apps/core/internal/corebanking/service.go`
- `apps/core/internal/corebanking/repository.go`
- `apps/core/internal/corebanking/sql_account_repository.go`
- `apps/core/internal/corebanking/sql_transfer_repository.go`
- `apps/core/internal/corebanking/service_test.go`
- `apps/core/internal/corebanking/handler_test.go`
- `apps/core/internal/corebanking/sql_repository_integration_test.go`

No migration was needed. The existing safe internal account shape is reused: `kind=internal`, `product_type=internal`, `allow_negative_balance=true`, `normal_balance=debit`, `status=active`.

## Endpoint

`POST /api/v1/internal/debits`

Example request:

```json
{
  "account_id": "d7a8f534-7f36-4fc9-b6e3-c54fc76f16f6",
  "amount_minor": 12000,
  "currency_id": "NGN",
  "idempotency_key": "manual-proof-debit-142511",
  "reference": "manual-proof-debit-ref-142511",
  "narration": "manual proof debit"
}
```

Example response:

```json
{
  "id": "1bee5a22-67ef-4526-b2f5-7259eebb99bc",
  "direction": "outbound",
  "status": "succeeded",
  "provider": "ledger_internal",
  "amount_minor": 12000,
  "journal_entry_id": "692bed0f-99cc-43a7-aa94-fd2c41868b70"
}
```

## Live HTTP And DB Evidence

Manual API proof against local Postgres:

```text
customer_id=7f95a639-30fb-49c4-86d6-53c6c6ad424d
account_id=d7a8f534-7f36-4fc9-b6e3-c54fc76f16f6
credit={"id":"d43d5c2c-852e-42db-bdd5-14ccc97a478f","direction":"inbound","status":"succeeded","provider":"ledger_internal","amount_minor":50000,"journal_entry_id":"86e75e70-265e-446c-a84f-25eb03ff3b0f"}
balance_after_credit={"available_minor":50000,"ledger_minor":50000}
debit={"id":"1bee5a22-67ef-4526-b2f5-7259eebb99bc","direction":"outbound","status":"succeeded","provider":"ledger_internal","amount_minor":12000,"journal_entry_id":"692bed0f-99cc-43a7-aa94-fd2c41868b70"}
duplicate_same_id=true
balance_after_debit={"available_minor":38000,"ledger_minor":38000}
insufficient_status=422 insufficient_body={"message":"insufficient_funds"} insufficient_transfer_count=0
transactions=[{"transfer_id":"1bee5a22-67ef-4526-b2f5-7259eebb99bc","direction":"outbound","status":"succeeded","signed_minor":-12000,"journal_entry_id":"692bed0f-99cc-43a7-aa94-fd2c41868b70"},{"transfer_id":"d43d5c2c-852e-42db-bdd5-14ccc97a478f","direction":"inbound","status":"succeeded","signed_minor":50000,"journal_entry_id":"86e75e70-265e-446c-a84f-25eb03ff3b0f"}]
db_transfer_row=outbound|succeeded|ledger_internal|12000|t
db_posting_rows=55555555-5555-5555-5555-555555555555|credit|12000
d7a8f534-7f36-4fc9-b6e3-c54fc76f16f6|debit|12000
```

## Concurrency Evidence

Targeted Postgres integration:

```text
=== RUN   TestSQLRepositoryInternalDebitIntegration
--- PASS: TestSQLRepositoryInternalDebitIntegration
=== RUN   TestSQLRepositoryInternalDebitConcurrentIdempotency
--- PASS: TestSQLRepositoryInternalDebitConcurrentIdempotency
=== RUN   TestSQLRepositoryInternalDebitConcurrentDistinctNoOverspend
--- PASS: TestSQLRepositoryInternalDebitConcurrentDistinctNoOverspend
PASS
ok   lenz-core/apps/core/internal/corebanking
```

The distinct-concurrent test funds `30000`, sends ten different debit keys for `7000`, and proves exactly four succeed, six return `ErrInsufficient`, final customer balance is `2000`, and reconciliation passes.

## Verification Commands

```text
go generate ./apps/core/internal/corebanking
go generate ./apps/core/internal/institution
git check-ignore -v apps/core/internal/corebanking/corebanking.gen.go apps/core/internal/institution/institution.gen.go
go test -race -count=1 ./apps/core/internal/corebanking
go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
go build ./apps/core/... ./apps/auth/... ./packages/shared/...
LENZ_INTEGRATION_DATABASE_URL='postgres://lenzcore:lenzcore123@localhost:55432/lenzcore?sslmode=disable' go test -count=1 -tags=integration ./apps/core/internal/corebanking -run 'TestSQLRepositoryInternalDebit(Integration|ConcurrentIdempotency|ConcurrentDistinctNoOverspend)' -v
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/demo_transfer_spine.sh
```

All passed.

Ignored generated-code proof:

```text
.gitignore:13:apps/core/internal/corebanking/corebanking.gen.go apps/core/internal/corebanking/corebanking.gen.go
.gitignore:14:apps/core/internal/institution/institution.gen.go apps/core/internal/institution/institution.gen.go
```

## Deferred Gaps

- No external provider/NIBSS flow in this build.
- No maker-checker, limits, KYC/BVN enforcement, fraud checks, or production RBAC in this build.
- Insufficient internal debit replays are not reserved as failed idempotency records; because no money action is accepted, a later retry after funding can succeed.
