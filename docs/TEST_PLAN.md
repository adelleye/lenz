# Test Plan

This is the durable verification checklist for the current Lenz Core
transaction spine.

## Default Proof

Run these from the repository root:

```sh
go generate ./apps/core/internal/corebanking
go generate ./apps/core/internal/institution
git check-ignore -v apps/core/internal/corebanking/corebanking.gen.go
git check-ignore -v apps/core/internal/institution/institution.gen.go
go test -race -count=1 ./apps/core/internal/corebanking
go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
go build ./apps/core/... ./apps/auth/... ./packages/shared/...
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/uat_simple_transaction_cba.sh
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/demo_transfer_spine.sh
```

Use another `POSTGRES_PORT` or `API_PORT` if the defaults are busy.

## What Must Stay True

Money correctness:

- every posted money movement has a balanced journal;
- `account_balances.ledger_minor` reconciles to postings;
- `available_minor` equals ledger minus active holds;
- standard customer accounts cannot spend below available balance;
- reversals create new ledger events instead of editing history.

Idempotency and concurrency:

- duplicate idempotency keys do not double-post;
- duplicate provider events do not double-credit;
- concurrent outbound requests cannot overspend;
- concurrent settlement or requery calls produce one money effect and no 500s
  for legitimate duplicate calls.

External mock lifecycle:

- pending outbound creates a hold and posts no journal;
- failed outbound releases the hold and posts no journal;
- successful outbound consumes the hold and posts exactly one journal;
- pending or provider-unknown requery keeps the hold and remains visible;
- successful inbound credits once;
- already-final transfers are deterministic no-ops;
- internal transfers are not settled through the external requery endpoint.

Security and tenant safety:

- non-health routes require bearer auth;
- `X-Institution-ID` cannot switch tenants;
- mismatched institution headers are rejected;
- query-string access tokens are rejected;
- demo routes are unavailable unless `LENZ_DEMO_MODE=true`;
- demo mode fails fast in production mode;
- internal errors return generic responses with request IDs.

Operational visibility:

- audit events exist for money-sensitive actions;
- reconciliation items expose pending, provider-unknown, failed, and
  manual-review cases;
- mark-reviewed metadata does not mutate money.

## Out Of Scope For This Prototype

- real provider integrations;
- provider webhook signature verification;
- production auth/RBAC;
- maker-checker;
- KYC/BVN/NIN verification;
- limits, fraud monitoring, statements, regulatory returns, and frontend flows;
- manual edits to generated OpenAPI files.

## Recording Results

Update [TEST_RESULTS.md](TEST_RESULTS.md) for major handoff runs with:

- date/time and commit;
- dirty-worktree note;
- exact commands run;
- pass/fail summary;
- important evidence snippets;
- failed or untested items;
- smallest next fixes.
