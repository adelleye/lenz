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

Start Postgres and Redis:

```sh
docker compose -f infra/docker/docker-compose.yml up -d postgres redis
```

Run migrations:

```sh
go run ./apps/core/cmd/migrate
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
