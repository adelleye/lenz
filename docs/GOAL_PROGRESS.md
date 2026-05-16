# Goal Progress

## 1. Repo Audit

What changed:
- Confirmed this is an early scaffold, not a mature core banking app.
- Found `apps/auth`, `apps/core`, `packages/shared`, server setup, Docker Compose, migrations, OpenAPI files, and an institution placeholder module.
- Found mismatches: docs and Dockerfile referenced `apps/backend`; actual API is `apps/core`. The initial down migration dropped `tenants`, which does not exist, and omitted current tables.

Command run:

```sh
rg --files -g '!*node_modules*' -g '!vendor' -g '!dist' -g '!build'
```

Result:
- Passed. Scaffold inventory completed.

What remains:
- No full PRD implementation attempted.

## 2. Local Run Path

What changed:
- Added `go.work` including `apps/auth`, `apps/core`, and `packages/shared`.
- Updated `go.work.example`.
- Fixed `apps/core/go.mod` local module requirements/replaces.
- Added a local Docker Compose default `DATABASE_URL` fallback.
- Fixed `infra/docker/Dockerfile.backend` to build `apps/core`, not `apps/backend`.
- Updated `Taskfile.yml` commands for Docker, migrations, API run, and tests.

Command run:

```sh
docker compose version
POSTGRES_PORT=55432 docker compose -f infra/docker/docker-compose.yml up -d postgres redis
DATABASE_URL='postgres://lenzcore:lenzcore123@localhost:55432/lenzcore?sslmode=disable' go run ./apps/core
```

Result:
- Passed after installing Docker CLI, Compose, and Colima via Homebrew.
- Used host port `55432` because this machine already had a native Postgres on `127.0.0.1:5432`.

What remains:
- None for local run path.

## 3. Migrations And Schema

What changed:
- Added `migrations/20260516000100_transaction_spine.up.sql`.
- Added tables for accounts, balances, journal entries, postings, transfers, provider events, and audit events.
- Preserved integer minor-unit money amounts with `bigint`.
- Added uniqueness for transfer idempotency and provider event dedupe.
- Fixed the existing fizz migration's broken institution index and down-table order.

Command run:

```sh
DATABASE_URL='postgres://lenzcore:lenzcore123@localhost:55432/lenzcore?sslmode=disable' go run ./apps/core/cmd/migrate
```

Result:
- Passed: `applied 20260516000100_transaction_spine`.

What remains:
- None.

## 4. Seed Data

What changed:
- Added `POST /api/v1/demo/seed`.
- Seed creates one demo institution, branch, customer, customer wallet account, and mock NIP clearing account.

Command run:

```sh
go test ./apps/core/internal/corebanking
```

Result:
- Passed. Unit tests seed demo data in the service layer.

What remains:
- Verify HTTP seeding with live Postgres once Docker is available.

## 5. Ledger Service

What changed:
- Added balanced journal-entry and posting creation.
- Customer balances are updated atomically from postings in the same SQL transaction.
- Reversals create new journal events.

Command run:

```sh
go test ./apps/core/internal/corebanking
```

Result:
- Passed.

What remains:
- Live Docker-backed seed verified with `POST /api/v1/demo/seed`.

## 6. Account Service

What changed:
- Added account listing by customer.
- Added balance lookup.
- All account reads are institution-scoped.

Command run:

```sh
go test ./apps/core/internal/corebanking
```

Result:
- Passed.

What remains:
- Live Docker-backed account and balance endpoints verified.

## 7. Mock Provider Adapter

What changed:
- Added mock NIP provider fields on transfers and provider events.
- Kept provider state internal to Lenz records so the adapter can later be replaced.

Command run:

```sh
go test ./apps/core/internal/corebanking
```

Result:
- Passed.

What remains:
- No real Monnify/Interswitch/NIBSS/sponsor-bank adapter was added.

## 8. Transfer-In

What changed:
- Added `POST /api/v1/transfers/mock/inbound`.
- Successful inbound transfers debit clearing and credit the customer wallet.
- Duplicate provider events do not double-credit.

Command run:

```sh
go test ./apps/core/internal/corebanking
```

Result:
- Passed.

What remains:
- Live Docker-backed mock provider requests verified.

## 9. Transfer-Out

What changed:
- Added `POST /api/v1/transfers/mock/outbound`.
- Successful outbound transfers debit the customer wallet and credit clearing.
- Insufficient funds create a failed transfer without postings.

Command run:

```sh
go test ./apps/core/internal/corebanking
```

Result:
- Passed.

What remains:
- Live Docker-backed outbound and insufficient-funds flows verified.

## 10. Transaction History

What changed:
- Added `GET /api/v1/accounts/:account_id/transactions`.
- Succeeded rows are generated from Lenz transfer, journal, and posting records.
- Pending and failed rows appear with zero signed movement.

Command run:

```sh
go test ./apps/core/internal/corebanking
```

Result:
- Passed.

What remains:
- Live Docker-backed transaction history verified.

## 11. Idempotency And Duplicate Protection

What changed:
- Every money-moving endpoint requires an idempotency key.
- Unique `(institution_id, idempotency_key)` prevents duplicate request posting.
- Unique `(institution_id, provider, provider_event_id)` prevents duplicate webhook/event crediting.

Command run:

```sh
go test ./apps/core/internal/corebanking
```

Result:
- Passed.

What remains:
- Race behavior should be stress-tested with live Postgres before production use.

## 12. Pending, Failed, And Reversal Flows

What changed:
- Pending and failed transfers are represented as transfer records without ledger postings.
- Reversal posts a new transfer and new balanced journal entry.

Command run:

```sh
go test ./apps/core/internal/corebanking
```

Result:
- Passed.

What remains:
- Live Docker-backed pending, failed, and reversal flows verified.

## 13. Tests

What changed:
- Added automated tests for all required scenarios:
  successful transfer-in, successful transfer-out, insufficient funds, duplicate idempotency key, duplicate provider event, pending history, failed transfer, reversal, tenant scoping, and Lenz-derived history.
- Added Postgres-backed integration coverage for the SQL store with aggregate checks for balanced journals and balances matching postings.

Command run:

```sh
/tmp/codex-go/go/bin/go test ./apps/core/... ./apps/auth/... ./packages/shared/...
```

Result:
- Passed.

What remains:
- None for current unit and SQL integration coverage.

## 14. Docs

What changed:
- Added `docs/TRANSFER_ENGINE_DEMO.md`.
- Updated `README.md`, `PROJECT_STRUCTURE.md`, `Taskfile.yml`, Dockerfile, migration config, and OpenAPI docs.
- Added `scripts/demo_transfer_spine.sh` as the single verified local proof command.

Command run:

```sh
GO_BIN=/tmp/codex-go/go/bin/go ./scripts/demo_transfer_spine.sh
```

Result:
- Passed: `DEMO TRANSFER SPINE: PASS`.

What remains:
- None for the transaction-spine demo proof.
