# Lenz Core

Lenz Core currently contains the Simple Transaction CBA v0.1 spine: a
multi-tenant, ledger-first core-banking slice for Nigerian MFB-style accounts.
It is production-shaped, but it is not a production-ready bank yet.

## What Is Built

The current API can run the basic customer and money lifecycle over real
Postgres tables with demo mode disabled:

- create customers and customer accounts with supplied unique 10-digit
  test/account numbers, not full NUBAN issuance;
- read account balances split into `ledger_minor` and `available_minor`;
- post internal credits, internal debits, and account-to-account transfers;
- write balanced double-entry journal entries and postings for posted money;
- prevent duplicate posting with idempotency keys;
- reject insufficient available balance;
- expose ledger-backed transaction history;
- place and release liens;
- freeze, unfreeze, activate post-no-debit, and deactivate post-no-debit with
  strict state transitions;
- write audit events for customer, account, money, and account-control actions;
- verify journal totals and account ledger balances reconcile to postings.

Mock/demo provider routes still exist for local transfer-provider experiments,
but they are separate from the v0.1 UAT path.

## What Is Not Built Yet

This is not ready to host real customers in production. The remaining production
work includes real auth/RBAC, maker-checker, limits, KYC/BVN/NIN verification,
true NUBAN generation/check-digit validation, which is deferred, real
provider/NIBSS/sponsor-bank adapters, signed webhooks, operational
reconciliation jobs, compliance reporting, monitoring, and deployment
hardening.

## Prove It Works

Prerequisites: Docker, Go, `curl`, and `jq`.

Run the fastest real-world local proof:

```sh
./scripts/uat_simple_transaction_cba.sh
```

That script creates a temporary Postgres database, runs migrations, bootstraps
an institution, branch, and internal settlement account, starts the API with
`LENZ_DEMO_MODE=false`, then verifies over HTTP and SQL:

- customer creation;
- account creation;
- zero opening balance;
- internal credit;
- internal debit;
- internal transfer between two customer accounts;
- transaction history on both accounts;
- lien behavior;
- post-no-debit behavior;
- freeze behavior;
- audit-event writes;
- balanced journals and account-ledger reconciliation.

Expected final line:

```text
UAT simple transaction CBA passed.
```

If port `55432` or `3001` is busy, override them:

```sh
POSTGRES_PORT=55433 API_PORT=3002 ./scripts/uat_simple_transaction_cba.sh
```

## Run The API Locally

Bootstrap a local tenant and settlement account:

```sh
POSTGRES_PORT=55432 ./scripts/bootstrap_cba_v0_1.sh
```

The script prints the environment variables to use. With the default values:

```sh
export DATABASE_URL='postgres://lenzcore:lenzcore123@localhost:55432/lenzcore?sslmode=disable'
export LENZ_DEV_AUTH_TOKEN='local-dev-token'
export LENZ_DEV_INSTITUTION_ID='11111111-1111-1111-1111-111111111111'
export APP_ENV='development'
export LENZ_DEMO_MODE='false'
export PORT='3001'

go run ./apps/core
```

Health check:

```sh
curl -fsS \
  -H 'Authorization: Bearer local-dev-token' \
  -H 'X-Institution-ID: 11111111-1111-1111-1111-111111111111' \
  http://localhost:3001/api/v1/health
```

For a complete HTTP walkthrough, read
[`scripts/uat_simple_transaction_cba.sh`](scripts/uat_simple_transaction_cba.sh).

## Generated OpenAPI Code

OpenAPI server code is generated locally and intentionally not committed.

- `design/openapi/core/corebanking.yaml` generates
  `apps/core/internal/corebanking/corebanking.gen.go`.
- `design/openapi/core/institution.yaml` generates
  `apps/core/internal/institution/institution.gen.go`.

Regenerate before direct `go test` or `go build` commands:

```sh
go generate ./apps/core/internal/corebanking
go generate ./apps/core/internal/institution
```

If `task` is installed:

```sh
task generate
```

## Other Verification

Run unit tests, race checks, and build:

```sh
go generate ./apps/core/internal/corebanking
go generate ./apps/core/internal/institution
go test -race -count=1 ./apps/core/internal/corebanking
go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
go build ./apps/core/... ./apps/auth/... ./packages/shared/...
```

The older mock-provider demo is still available:

```sh
./scripts/demo_transfer_spine.sh
```

Use it only for demo/mock provider behavior. Use
`./scripts/uat_simple_transaction_cba.sh` to prove the current non-demo CBA v0.1
spine.

## Local Security Notes

Authenticated local requests use the dev bearer token only. `X-Institution-ID`
is a consistency check against the authenticated institution, not a source of
truth. Do not pass access tokens in query strings.

CORS is explicit. Set allowed browser origins as a comma-separated list:

```sh
export LENZ_CORS_ALLOWED_ORIGINS='http://localhost:5173,http://127.0.0.1:5173'
```

Wildcard production CORS is rejected at startup. `LENZ_DEMO_MODE=true` also
fails fast when `APP_ENV` or `ENV` is production.
