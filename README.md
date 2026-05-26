# Lenz Core

Lenz Core is a Go/Postgres core-banking prototype for Nigerian MFB-style account
and transfer operations. The current product slice is transfer-first: a
BankOne-style replacement path for money in, money out, balances, transfer
history, and reconciliation.

It is ledger-first. Posted money movement is recorded as balanced journal
entries and postings, while account balances are cached views of that ledger.

This is useful for proving the transaction spine. It is not production-ready
banking software yet.

## Where We Are

We have a verified local transaction spine running on real Postgres tables. It
can create customers/accounts, move money internally, simulate external
inbound/outbound transfers through a mock NIP-shaped provider, keep transfer
history, hold pending outbound money, and surface reconciliation/manual-review
cases.

The next useful product step is to turn the mock provider boundary into a real
provider integration path, while adding the production controls that real money
requires: auth/RBAC, maker-checker, limits, KYC/BVN/NIN, signed webhooks,
monitoring, and operating procedures.

## What We Built

The current API can run these flows over real Postgres tables:

- create customers and customer accounts with supplied unique 10-digit test
  account numbers;
- read ledger and available balances;
- post internal credits, internal debits, and account-to-account transfers;
- reject insufficient available balance;
- keep pending outbound transfers on hold without posting ledger money;
- consume holds and post exactly once when a pending outbound succeeds;
- release holds without posting when a pending outbound fails;
- ingest mock external inbound events and credit successful inbound transfers
  once;
- requery pending or provider-unknown mock external transfers;
- expose transaction history, transfer lookup, admin transfer lists, journal
  inspection, and reconciliation/manual-review queues;
- freeze, unfreeze, apply post-no-debit, and manage liens;
- write audit events for customer, account, money, account-control, and
  reconciliation actions.

The external transfer paths are mock provider flows only. They exercise the
provider/ledger/reconciliation boundary without connecting to real NIBSS,
BankOne, Monnify, Interswitch, Providus, or a sponsor bank.

## What Is Not Built

This is not a full BankOne replacement yet. Before real customer money, the
product still needs:

- production auth/RBAC and tenant/user role enforcement;
- maker-checker and limit checks;
- KYC/BVN/NIN verification;
- true NUBAN issuance/check-digit validation;
- real NIBSS/NIP or sponsor-bank provider adapters;
- signed webhooks and provider credential management;
- provider settlement files and operations workflows;
- compliance reporting, monitoring, deployment hardening, and runbooks.

## Prove It Works

Prerequisites: Go, Docker, `curl`, and `jq`.

Run the CBA v0.1 proof:

```sh
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/uat_simple_transaction_cba.sh
```

Expected final line:

```text
UAT simple transaction CBA passed.
```

Run the fuller transfer-spine demo, including mock provider flows and requery:

```sh
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/demo_transfer_spine.sh
```

Expected final line:

```text
DEMO TRANSFER SPINE: PASS
```

For code-level verification:

```sh
go generate ./apps/core/internal/corebanking
go generate ./apps/core/internal/institution
git check-ignore -v apps/core/internal/corebanking/corebanking.gen.go
git check-ignore -v apps/core/internal/institution/institution.gen.go
go test -race -count=1 ./apps/core/internal/corebanking
go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
go build ./apps/core/... ./apps/auth/... ./packages/shared/...
```

If ports are busy, override them:

```sh
TMPDIR=$PWD/tmp POSTGRES_PORT=55433 API_PORT=3002 ./scripts/uat_simple_transaction_cba.sh
```

## Run The API Locally

Bootstrap a local tenant, branch, and settlement account:

```sh
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/bootstrap_cba_v0_1.sh
```

The script prints the environment variables to use. With the defaults:

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

## How The Code Connects

- `design/openapi/core/corebanking.yaml` is the active OpenAPI source of truth
  for the transaction CBA v0.1 HTTP API.
- `design/openapi/core/institution.yaml` is an intentional generated-module
  placeholder. It defines no routes and is not registered by the runtime API.
- `go generate` creates ignored `*.gen.go` server/model files.
- `apps/core/server` wires auth, tenant checks, CORS, routes, and dependencies.
- `apps/core/internal/corebanking/handler*.go` adapts HTTP requests into service
  calls.
- `apps/core/internal/corebanking/service*.go` owns business decisions:
  account policy, holds, ledger posting, provider status, reconciliation, and
  audit.
- `apps/core/internal/corebanking/sql_*.go` owns Postgres reads and writes.
- `migrations/` defines the database schema.
- `scripts/` contains the repeatable local proof flows.

See [PROJECT_STRUCTURE.md](PROJECT_STRUCTURE.md) for a folder-by-folder map and
[docs/README.md](docs/README.md) for the documentation map.

## Generated OpenAPI Code

Generated files are intentionally not committed:

- `apps/core/internal/corebanking/corebanking.gen.go`
- `apps/core/internal/institution/institution.gen.go`

Active OpenAPI inputs:

- `design/openapi/core/corebanking.yaml`: active runtime contract for CBA v0.1
  customer, account, ledger, transfer, reconciliation, audit, and mock-provider
  routes.
- `design/openapi/core/institution.yaml`: no-route placeholder kept only so the
  institution module generation path stays explicit until institution product
  APIs are designed.

Regenerate them before direct test/build commands:

```sh
go generate ./apps/core/internal/corebanking
go generate ./apps/core/internal/institution
```

## Local Security Notes

Local authenticated requests use a development bearer token. `X-Institution-ID`
must match the authenticated institution; it is not trusted as the source of
truth. Query-string access tokens are rejected.

Demo-only seed/mock routes require `LENZ_DEMO_MODE=true` and are blocked in
production mode. CORS is explicit through `LENZ_CORS_ALLOWED_ORIGINS`.
