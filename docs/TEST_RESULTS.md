# Test Results

Latest verified state for the current transaction spine.

## Run Summary

- Date: 2026-05-26 WAT.
- Commit: `d0d82ee6c4ad050ae2f03f782db15bae424acd7a`.
- Branch used for implementation: `goal/cba-v0.1-14-mock-tsq-requery`.
- Result: pass.
- Scope: mock transaction status query/requery plus the existing CBA v0.1
  transaction spine.

## Commands Run

| Command | Result | Evidence |
| --- | --- | --- |
| `go generate ./apps/core/internal/corebanking` | PASS | exit 0 |
| `go generate ./apps/core/internal/institution` | PASS | exit 0 |
| `git check-ignore -v apps/core/internal/corebanking/corebanking.gen.go` | PASS | `.gitignore:13` |
| `git check-ignore -v apps/core/internal/institution/institution.gen.go` | PASS | `.gitignore:14` |
| `go test -race -count=1 ./apps/core/internal/corebanking` | PASS | race suite passed |
| `go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...` | PASS | module suite passed |
| `go build ./apps/core/... ./apps/auth/... ./packages/shared/...` | PASS | exit 0 |
| `TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/demo_transfer_spine.sh` | PASS | `DEMO TRANSFER SPINE: PASS` |
| `TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/uat_simple_transaction_cba.sh` | PASS | `UAT simple transaction CBA passed.` |

## Behavior Proved

- generated OpenAPI files regenerate locally and remain ignored;
- account/customer/internal-money flows work over real Docker Postgres;
- journals balance and account ledger balances reconcile to postings;
- pending outbound transfers create holds without posting ledger money;
- failed pending outbound transfers release holds and post no journal;
- successful pending outbound transfers consume holds and post once;
- mock external inbound success credits once;
- mock external pending, failed, unknown, and mismatch cases remain visible for
  reconciliation without posting incorrect money;
- requery is limited to pending/provider-unknown external transfers;
- already-final requery calls are deterministic no-ops;
- internal transfers are not settled through the external requery endpoint;
- 10 concurrent requery calls produce one money effect and no 500s;
- audit, tenant/auth checks, and reconciliation separation remain in place.

## Residual Gaps

The verified result is still a prototype. These are not implemented:

- real provider adapters;
- signed provider webhooks;
- production auth/RBAC;
- maker-checker;
- KYC/BVN/NIN verification;
- true NUBAN issuance;
- limits and fraud monitoring;
- statements, regulatory returns, and production operations.

## Handoff Judgement

Safe to hand off as a verified local transaction-spine prototype.

Not safe to represent as production banking software until the residual gaps
above are designed, implemented, and verified.
