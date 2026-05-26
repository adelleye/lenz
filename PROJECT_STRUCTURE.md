# Lenz Core Project Structure

This is the quick map of the repository as it exists now.

## Top-Level Folders

```text
apps/
  core/        Go API, migrations runner, HTTP server, core-banking module
  auth/        Local auth/authz scaffolding used by the core API
design/
  openapi/     OpenAPI specs used to generate server/model code
docs/          Current guides, test notes, and historical goal briefs
infra/
  docker/      Local Docker Compose services and API Dockerfile
migrations/    SQL schema changes
packages/
  shared/      Shared config, middleware, and utility packages
scripts/       Bootstrap, UAT, and demo proof scripts
```

## Core API Shape

`apps/core` is the main application.

```text
apps/core/
  main.go                         starts the API
  server/                         HTTP server, middleware, dependency wiring
  internal/corebanking/           CBA v0.1 transaction spine
  internal/institution/           no-route OpenAPI module placeholder
```

Active OpenAPI inputs:

- `design/openapi/core/corebanking.yaml` is the current source of truth for the
  transaction CBA v0.1 HTTP surface and the runtime routes registered by
  `apps/core`.
- `design/openapi/core/institution.yaml` is intentionally a placeholder with no
  routes. It is generated only to keep the institution module workflow visible;
  it does not define runtime API behavior yet.

The core-banking module is split by responsibility:

```text
internal/corebanking/
  handler*.go                     HTTP boundary and generated strict handlers
  service*.go                     business rules and orchestration
  repository.go                   repository interfaces
  sql_*.go                        Postgres implementations
  provider*.go, mock_nip_provider.go
                                  provider adapter boundary and mock provider
  audit*.go                       audit event contracts and writes
  *_test.go                       unit, race, and SQL integration coverage
```

The intended request path is:

```text
OpenAPI spec -> generated strict handler -> handwritten handler -> service -> repository -> Postgres
```

## Important Database Tables

Tenant-scoped banking tables include:

- `customers`
- `accounts`
- `account_balances`
- `account_holds`
- `journal_entries`
- `postings`
- `transfers`
- `provider_events`
- `audit_events`
- `reconciliation_reviews`

The ledger source of truth is `journal_entries` plus `postings`.
`account_balances` is a cache for fast reads.

## Main API Areas

All routes are under `/api/v1`.

Customer and account:

- `POST /customers`
- `GET /customers/{customer_id}`
- `POST /customers/{customer_id}/accounts`
- `GET /accounts`
- `GET /accounts/{account_id}`
- `GET /accounts/{account_id}/balance`
- `GET /accounts/{account_id}/transactions`

Account creation currently validates a supplied unique 10-digit test account
number. Full NUBAN generation/check-digit validation is deferred.

Account controls:

- `POST /accounts/{account_id}/freeze`
- `POST /accounts/{account_id}/unfreeze`
- `POST /accounts/{account_id}/post-no-debit`
- `DELETE /accounts/{account_id}/post-no-debit`
- `POST /accounts/{account_id}/liens`
- `DELETE /accounts/{account_id}/liens/{lien_id}`

Money movement:

- `POST /internal/credits`
- `POST /internal/debits`
- `POST /internal/transfers`
- `POST /external/name-enquiry`
- `POST /external/transfers/outbound`
- `POST /external/transfers/inbound-events`
- `POST /external/transfers/{transfer_id}/requery`

Admin and reconciliation:

- `GET /transfers/{transfer_id}`
- `POST /transfers/{transfer_id}/reverse`
- `GET /admin/transfers`
- `GET /admin/ledger/journal/{journal_entry_id}`
- `GET /admin/reconciliation-items`
- `GET /admin/reconciliation-items/{transfer_id}`
- `POST /admin/reconciliation-items/{transfer_id}/mark-reviewed`

Demo-only routes, enabled only with `LENZ_DEMO_MODE=true`:

- `POST /demo/seed`
- `POST /transfers/mock/inbound`
- `POST /transfers/mock/outbound`

## Common Commands

```sh
go generate ./apps/core/internal/corebanking
go generate ./apps/core/internal/institution
go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
go build ./apps/core/... ./apps/auth/... ./packages/shared/...
./scripts/migrate.sh up
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/uat_simple_transaction_cba.sh
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/demo_transfer_spine.sh
```

## Current Boundary

The repository proves the local transaction spine with a mock provider. It does
not yet include real provider credentials, signed webhooks, production auth,
maker-checker, limits, KYC/BVN/NIN verification, deployment hardening, or
regulatory reporting.
