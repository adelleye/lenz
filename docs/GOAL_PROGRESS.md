# Goal Progress

This is a historical build log. Use the README and docs index for current run
instructions.

## Completed Build Areas

1. Repo/local run path: Go workspace, Docker Compose, migration runner, API run
   commands, and generated OpenAPI workflow.
2. Database spine: institutions, branches, customers, accounts, balances,
   journal entries, postings, transfers, provider events, holds, audit events,
   and reconciliation metadata.
3. Customer/account flows: customer creation, account creation, balance reads,
   and ledger-backed transaction history.
4. Internal money movement: credits, debits, and account-to-account transfers
   with idempotency and balanced postings.
5. Account controls: freeze, unfreeze, post-no-debit, and liens.
6. Audit events for customer, account, money, control, and reconciliation
   actions.
7. Reconciliation/manual-review queues and mark-reviewed metadata.
8. Mock provider boundary: name enquiry, outbound lifecycle, inbound events,
   provider-event duplicate protection, and transaction status requery.
9. Demo/UAT proof scripts:
   - `scripts/uat_simple_transaction_cba.sh`
   - `scripts/demo_transfer_spine.sh`

## Current Boundary

The repo proves the local transaction spine with a mock provider. It does not
yet implement real provider rails, production auth/RBAC, maker-checker,
KYC/BVN/NIN verification, limits, fraud monitoring, statements, regulatory
returns, or production deployment.

## Current Proof Commands

```sh
go generate ./apps/core/internal/corebanking
go test -race -count=1 ./apps/core/internal/corebanking
go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
go build ./apps/core/... ./apps/auth/... ./packages/shared/...
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/uat_simple_transaction_cba.sh
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/demo_transfer_spine.sh
```
