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

## Verification

```sh
go test ./apps/core/... ./apps/auth/... ./packages/shared/...
```
