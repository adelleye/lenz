# CBA v0.1 Build 06: Internal Account Transfer Proof

## Scope

Implemented `POST /api/v1/internal/transfers` as a production-shaped same-institution, same-currency account transfer:

- debits the source customer account
- credits the destination customer account
- writes one balanced journal with two postings
- updates both `ledger_minor` and `available_minor`
- shows an outflow in source history and inflow in destination history
- returns the existing transfer for duplicate idempotency keys
- rejects insufficient funds without creating a transfer, journal, or postings
- uses deterministic account locking for two-account transfers

No migration was needed. This reuses the existing transfer, journal, posting, and balance tables.

## Files Changed

- `design/openapi/core/corebanking.yaml`
- `apps/core/internal/corebanking/handler.go`
- `apps/core/internal/corebanking/model.go`
- `apps/core/internal/corebanking/service.go`
- `apps/core/internal/corebanking/sql_account_repository.go`
- `apps/core/internal/corebanking/sql_transfer_repository.go`
- `apps/core/internal/corebanking/service_test.go`
- `apps/core/internal/corebanking/handler_test.go`
- `apps/core/internal/corebanking/sql_repository_integration_test.go`

## Endpoint

`POST /api/v1/internal/transfers`

Example request:

```json
{
  "source_account_id": "c4132d47-2dbf-4c6f-84be-3d88b22fd001",
  "destination_account_id": "0d8ee5de-ac15-4c8b-be25-4ff0e3ee5adc",
  "amount_minor": 12000,
  "currency_id": "NGN",
  "idempotency_key": "manual-proof-transfer-143619",
  "reference": "manual-proof-transfer-ref-143619",
  "narration": "manual account transfer"
}
```

Example response:

```json
{
  "id": "93a6bb90-b246-4ccf-ac82-e458a65bff8d",
  "direction": "outbound",
  "status": "succeeded",
  "provider": "ledger_internal",
  "amount_minor": 12000,
  "journal_entry_id": "103f7b7d-8b5c-4773-98c7-12c31fd6cda6"
}
```

## Live HTTP And DB Evidence

Manual API proof against local Postgres:

```text
source_account_id=c4132d47-2dbf-4c6f-84be-3d88b22fd001
destination_account_id=0d8ee5de-ac15-4c8b-be25-4ff0e3ee5adc
credit={"id":"3ff92520-68c7-4542-ae5c-3eb4d6c11fbc","direction":"inbound","status":"succeeded","provider":"ledger_internal","amount_minor":50000,"journal_entry_id":"981f437e-8c8d-46e5-b97a-2a0aaea11007"}
transfer={"id":"93a6bb90-b246-4ccf-ac82-e458a65bff8d","direction":"outbound","status":"succeeded","provider":"ledger_internal","amount_minor":12000,"journal_entry_id":"103f7b7d-8b5c-4773-98c7-12c31fd6cda6"}
duplicate_same_id=true
source_balance={"available_minor":38000,"ledger_minor":38000}
destination_balance={"available_minor":12000,"ledger_minor":12000}
insufficient_status=422 insufficient_body={"message":"insufficient_funds"} insufficient_transfer_count=0
source_transactions=[{"transfer_id":"93a6bb90-b246-4ccf-ac82-e458a65bff8d","direction":"debit","status":"succeeded","signed_amount_minor":-12000,"journal_entry_id":"103f7b7d-8b5c-4773-98c7-12c31fd6cda6"},{"transfer_id":"3ff92520-68c7-4542-ae5c-3eb4d6c11fbc","direction":"credit","status":"succeeded","signed_amount_minor":50000,"journal_entry_id":"981f437e-8c8d-46e5-b97a-2a0aaea11007"}]
destination_transactions=[{"transfer_id":"93a6bb90-b246-4ccf-ac82-e458a65bff8d","direction":"credit","status":"succeeded","signed_amount_minor":12000,"journal_entry_id":"103f7b7d-8b5c-4773-98c7-12c31fd6cda6"}]
db_balances=0d8ee5de-ac15-4c8b-be25-4ff0e3ee5adc|12000|12000
c4132d47-2dbf-4c6f-84be-3d88b22fd001|38000|38000
db_transfer=93a6bb90-b246-4ccf-ac82-e458a65bff8d|12000|manual-proof-transfer-143619|103f7b7d-8b5c-4773-98c7-12c31fd6cda6
db_postings=0d8ee5de-ac15-4c8b-be25-4ff0e3ee5adc|credit|12000
c4132d47-2dbf-4c6f-84be-3d88b22fd001|debit|12000
db_posting_totals=credit|12000
debit|12000
```

## Concurrency Evidence

Targeted Postgres integration:

```text
=== RUN   TestSQLRepositoryInternalTransferIntegration
--- PASS: TestSQLRepositoryInternalTransferIntegration
=== RUN   TestSQLRepositoryInternalTransferConcurrentIdempotency
--- PASS: TestSQLRepositoryInternalTransferConcurrentIdempotency
=== RUN   TestSQLRepositoryInternalTransferConcurrentDistinctNoOverspend
--- PASS: TestSQLRepositoryInternalTransferConcurrentDistinctNoOverspend
PASS
ok   lenz-core/apps/core/internal/corebanking
```

The distinct-concurrent test funds the source with `30000`, sends ten different transfer keys for `7000`, and proves exactly four succeed, six return `ErrInsufficient`, final source balance is `2000`, final destination balance is `28000`, and reconciliation passes.

## Verification Commands

```text
go generate ./apps/core/internal/corebanking
go generate ./apps/core/internal/institution
git check-ignore -v apps/core/internal/corebanking/corebanking.gen.go apps/core/internal/institution/institution.gen.go
go test -race -count=1 ./apps/core/internal/corebanking
go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
go build ./apps/core/... ./apps/auth/... ./packages/shared/...
LENZ_INTEGRATION_DATABASE_URL='postgres://lenzcore:lenzcore123@localhost:55432/lenzcore?sslmode=disable' go test -count=1 -tags=integration ./apps/core/internal/corebanking -run 'TestSQLRepositoryInternalTransfer(Integration|ConcurrentIdempotency|ConcurrentDistinctNoOverspend)' -v
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/demo_transfer_spine.sh
```

All passed.

Ignored generated-code proof:

```text
.gitignore:13:apps/core/internal/corebanking/corebanking.gen.go apps/core/internal/corebanking/corebanking.gen.go
.gitignore:14:apps/core/internal/institution/institution.gen.go apps/core/internal/institution/institution.gen.go
```

## Deferred Gaps

- This build supports active customer-to-customer NGN accounts only.
- No external provider, NIBSS, fees, scheduled transfers, bulk transfers, maker-checker, fraud controls, or frontend.
- Insufficient transfer attempts do not reserve failed idempotency rows; because no money action is accepted, a later retry after funding can succeed.
