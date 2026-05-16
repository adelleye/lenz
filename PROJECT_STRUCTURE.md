# Lenz Core - Project Structure

This document outlines the complete monorepo structure for Lenz Core Banking Application.

## Directory Structure

```
lenz-core/
├── apps/
│   ├── core/                       # Go core banking API
│   │   ├── cmd/migrate/            # SQL migration runner
│   │   ├── internal/corebanking/    # First transaction-spine slice
│   │   ├── internal/institution/    # Institution placeholder module
│   │   ├── server/                 # HTTP server setup
│   │   ├── main.go
│   │   ├── go.mod
│   │   └── go.sum
│   └── auth/                       # Auth scaffolding
│       ├── authn/
│       ├── authz/
│       ├── main.go
│       └── go.mod
│
├── packages/
│   └── shared/                     # Shared code
│       ├── config/
│       ├── httpmiddleware/
│       └── utils/
│
├── infra/
│   ├── docker/
│   │   ├── docker-compose.yml      # Local development services
│   │   └── Dockerfile.backend      # Core API Docker image
├── migrations/                     # Database migrations
├── design/openapi/                 # OpenAPI source files
│
├── docs/
│   ├── GOAL_PROGRESS.md
│   └── TRANSFER_ENGINE_DEMO.md
├── README.md                       # Main README
├── Taskfile.yml                    # Local task shortcuts
├── go.work
└── PROJECT_STRUCTURE.md            # This file

```

## Key Components

### Core API (`apps/core/`)

**Technology Stack:**
- Go 1.21+
- sqlx
- PostgreSQL
- Chi Router

**Modules:**
- `corebanking`: First transaction-spine slice for accounts, ledger postings,
  mock transfers, idempotency, transaction history, and reversals
- `ops`: Background jobs, cron scheduler

### Auth (`apps/auth/`)

Lightweight auth middleware scaffolding used by the core API.

### Shared Packages (`packages/shared/`)

- Common data models
- Validation utilities
- UUID helpers
- Shared types/interfaces

### Infrastructure (`infra/`)

- **Docker**: Development environment setup
- **Migrations**: Database schema versioning
- **Scripts**: Deployment automation

## Database Schema

Key tables are scoped by `institution_id` where they touch tenant data:

- `institutions` - Tenant banks
- `branches` - Bank branches
- `customers` - Bank customers
- `accounts` - Bank accounts (NUBAN)
- `journal_entries` - Double-entry ledger entries
- `postings` - Ledger posting lines
- `account_balances` - Account balances
- `transfers` - Money transfers
- `provider_events` - Mock provider webhook/event dedupe
- `audit_events` - Audit trail

## API Structure

All endpoints under `/api/v1/`:

- `GET /health` - Health check
- `POST /demo/seed` - Seed demo data
- `GET /customers/{customer_id}/accounts` - Customer accounts
- `GET /accounts/{account_id}/balance` - Account balance
- `GET /accounts/{account_id}/transactions` - Lenz transaction history
- `POST /transfers/mock/inbound` - Mock transfer-in
- `POST /transfers/mock/outbound` - Mock transfer-out
- `GET /transfers/{transfer_id}` - Transfer lookup
- `POST /transfers/{transfer_id}/reverse` - Reversal
- `GET /admin/ledger/journal/{journal_entry_id}` - Journal inspection
- `GET /admin/transfers` - Admin transfer list

## Development Commands

See `Taskfile.yml` or run the commands directly:

- `docker compose -f infra/docker/docker-compose.yml up -d postgres redis` - Start local services
- `go run ./apps/core/cmd/migrate` - Run SQL migrations
- `go run ./apps/core` - Start core API
- `go test ./apps/core/... ./apps/auth/... ./packages/shared/...` - Run tests

## Next Steps

1. Run the documented demo flow against Docker-backed Postgres
2. Add live integration tests for the SQL store
3. Add real auth and institution context middleware
4. Replace the mock provider with production adapters behind the same boundary

## Notes

- `apps/core/internal/corebanking` contains the first real vertical slice
- SQL migrations for the transfer engine are ready to run
- Docker Compose setup is complete
- Tenant scoping is enforced with `institution_id` in SQL queries and tests
