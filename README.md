# Lenz Core - Core Banking Application

A next-generation, multi-tenant Core Banking Application for Nigeria (and beyond).

## Overview
This repository currently contains the first working transaction-spine slice for
Lenz Core:

- demo institution, branch, customer, customer account, and internal mock NIP
  clearing account
- integer-minor-unit account balances
- balanced double-entry journal entries and postings
- mock inbound and outbound transfers
- request idempotency and provider-event duplicate protection
- pending, failed, and reversal transfer flows
- ledger-derived account transaction history

## Local Demo

The fastest verified proof is:

```sh
./scripts/demo_transfer_spine.sh
```

This resets the demo Docker database volume, runs migrations, runs unit and
Postgres-backed integration tests, starts the API, and asserts the transfer
spine over HTTP.

Start Postgres and Redis:

```sh
docker compose -f infra/docker/docker-compose.yml up -d postgres redis
```

Run migrations:

```sh
go run ./apps/core/cmd/migrate
```

If you already have local Postgres on port 5432, start Compose with
`POSTGRES_PORT=55432` and set:

```sh
export DATABASE_URL='postgres://lenzcore:lenzcore123@localhost:55432/lenzcore?sslmode=disable'
```

Start the API:

```sh
go run ./apps/core
```

Then follow [docs/TRANSFER_ENGINE_DEMO.md](docs/TRANSFER_ENGINE_DEMO.md) for
the exact API calls and expected output shape.

## Generated OpenAPI Code

OpenAPI server code is generated locally and intentionally not committed.

- `design/openapi/core/corebanking.yaml` generates
  `apps/core/internal/corebanking/corebanking.gen.go`.
- `design/openapi/core/institution.yaml` generates
  `apps/core/internal/institution/institution.gen.go`.

Regenerate both files before direct `go test` or `go build` commands:

```sh
go generate ./apps/core/internal/corebanking
go generate ./apps/core/internal/institution
```

If `task` is installed, use:

```sh
task generate
```

The Taskfile `test`, `build`, and `demo_transfer_spine` tasks run generation
first.

## Security-Sensitive Local Configuration

Authenticated local requests use the dev bearer token only:

```sh
export LENZ_DEV_AUTH_TOKEN='choose-a-local-token'
export LENZ_DEV_INSTITUTION_ID='11111111-1111-1111-1111-111111111111'
```

`X-Institution-ID` is optional and is only a consistency check against the
authenticated principal institution. Do not pass access tokens in query strings.

CORS is explicit. Set allowed browser origins as a comma-separated list:

```sh
export LENZ_CORS_ALLOWED_ORIGINS='http://localhost:5173,http://127.0.0.1:5173'
```

Production must configure concrete origins. Wildcard production CORS is rejected
at startup.

Demo/mock mutation routes are disabled by default. To run the local transfer
spine demo:

```sh
export LENZ_DEMO_MODE=true
export APP_ENV=development
```

`LENZ_DEMO_MODE=true` fails fast when `APP_ENV` or `ENV` is production.

## Verification

```sh
task test
```

Without `task`, run the same flow directly:

```sh
go generate ./apps/core/internal/corebanking
go generate ./apps/core/internal/institution
go test ./apps/core/... ./apps/auth/... ./packages/shared/...
```
