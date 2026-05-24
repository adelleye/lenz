# Lenz Core Transaction Spine Test Plan

This plan is the durable operator/tester checklist for the current Lenz Core
transaction-spine prototype. It is intentionally broader than unit testing: the
default proof path must exercise generated OpenAPI code, a clean Docker
Postgres database, migrations, the API server, HTTP calls, and direct SQL
reconciliation checks.

## Scope

Priority order:

1. Money correctness.
2. Security and tenant isolation.
3. Provider safety.
4. Operational readiness.
5. Performance and readiness signals.

In scope:

- Go/OpenAPI generation workflow.
- Docker/Postgres startup from a clean database.
- Migration application and repeatability.
- API health, auth, CORS, demo-mode gates, and request validation.
- Demo seed, inbound, outbound, pending, failed, reversal, history, admin list,
  and admin journal flows.
- Direct SQL reconciliation of journals, postings, account balances, active
  holds, uniqueness constraints, and expected indexes.
- Idempotency, provider-event duplicate protection, and concurrency probes.
- Current mock provider behavior and the absence of fallback-provider scaffolding.
- Empty-list and response-shape checks for OpenAPI-backed list endpoints.
- Read-only unused-code sweep for cleanup candidates.
- Handoff readiness judgement and smallest next fixes.

Out of scope unless a later goal expands it:

- Real provider integrations.
- Frontend or customer app flows.
- Production auth/RBAC, maker-checker, KYC/BVN, fraud, regulatory reporting, or
  real provider signature verification.
- Loans, overdraft engine, fees, statements, reconciliation workers, or limits.
- Manual edits to generated OpenAPI files.

## Required Local Environment

- Go available as `go`.
- Docker engine and Docker Compose available.
- `jq` and `curl` available.
- Local Postgres port `55432` available, or override `POSTGRES_PORT`.

Default database URL used by the proof:

```sh
postgres://lenzcore:lenzcore123@localhost:55432/lenzcore?sslmode=disable
```

## Baseline Command Sequence

Run from the repository root:

```sh
go generate ./apps/core/internal/corebanking
go generate ./apps/core/internal/institution
git check-ignore -v apps/core/internal/corebanking/corebanking.gen.go
git check-ignore -v apps/core/internal/institution/institution.gen.go
go test -race ./apps/core/internal/corebanking
go test ./apps/core/... ./apps/auth/... ./packages/shared/...
go build ./apps/core/... ./apps/auth/... ./packages/shared/...
scripts/demo_transfer_spine.sh
```

Then run migrations again against the same database to prove idempotency:

```sh
DATABASE_URL='postgres://lenzcore:lenzcore123@localhost:55432/lenzcore?sslmode=disable' \
  go run ./apps/core/cmd/migrate
```

## End-To-End Flow

The core demo proof must start from a clean Docker volume:

1. Reset Docker Compose services and volumes.
2. Start Postgres and Redis.
3. Wait for both containers to become healthy.
4. Apply migrations from scratch.
5. Run the normal Go tests.
6. Run Postgres-backed integration tests.
7. Build and start the API with:
   - `APP_ENV=development`
   - `LENZ_DEMO_MODE=true`
   - `LENZ_DEV_AUTH_TOKEN=<throwaway token>`
   - `LENZ_DEV_INSTITUTION_ID=11111111-1111-1111-1111-111111111111`
8. Call `/api/v1/health` without auth.
9. Seed demo tenant data.
10. Run inbound, outbound, pending, failed, reversal, history, transfer-list,
    and journal-read flows over HTTP.
11. Confirm every posted journal balances by HTTP and direct SQL.
12. Confirm account balance caches reconcile to postings minus active holds.

## Scenario Checklist

### OpenAPI And HTTP Boundary

- Generated files are ignored by git.
- `go generate` recreates generated code without manual edits.
- `HTTPServer` implements the generated strict interface.
- Routes use `NewStrictHandlerWithOptions(...)`.
- Invalid path/header/body shapes are rejected before money logic runs.

### Local Runtime

- Clean Docker Postgres starts and is healthy.
- Migrations apply from scratch and can be rerun safely.
- API starts on a test port.
- Health endpoint works without auth.
- Non-health endpoints require bearer auth.

### Money Flow

- Demo seed creates institution, branch, customer, customer account, and clearing
  account.
- Inbound credit posts a balanced journal and increases balance.
- Outbound debit posts a balanced journal and decreases balance.
- Pending outbound creates a hold, reducing available but not ledger balance.
- Failed pending outbound releases the hold without posting.
- Successful pending outbound consumes the hold and posts once.
- Pending inbound appears in history without increasing balance.
- Reversal creates a new transfer and journal; original transfer remains.
- Reversal deficit is explicit with `ledger_status=reversal_deficit` and
  `reconciliation_status=manual_review`.

### Duplicate And Concurrency

- Same inbound idempotency key does not double-credit.
- Same outbound idempotency key does not double-debit.
- Same provider event does not double-credit.
- Same provider event replayed concurrently must not double-credit and should
  return deterministic replay responses, not 500s.
- Same idempotency key sent concurrently must not double-post and should return
  deterministic replay responses, not 500s.
- Concurrent outbound transfers from the same balance must not overspend.
- Outbound and reversal racing on the same account must leave balanced journals
  and reconciled balances.
- Concurrent provider settlement/requery must not double-post and should not
  return 500s for legitimate duplicate settlement.

### Security And Abuse

- Institution scope comes from the authenticated principal.
- Mismatched `X-Institution-ID` is rejected.
- Caller cannot switch tenant by changing `X-Institution-ID`.
- Query-string `access_token` is rejected.
- CORS allows only configured safe dev origins and rejects untrusted origins.
- Demo routes are disabled by default and cannot mutate money when off.
- `LENZ_DEMO_MODE=true` must fail startup in production.
- Unexpected internal errors return a generic 500 with a request ID.
- Raw DB/provider/internal strings must not be exposed to clients.
- Invalid UUIDs, negative amounts, zero amounts, missing idempotency keys,
  unknown statuses/scenarios, and oversized request bodies are rejected.

### Database Readiness

- Required indexes exist:
  - `transfers_pending_provider_reference_idx`
  - `transfers_institution_created_idx`
  - `transfers_account_idx`
  - `transfers_institution_id_idempotency_key_key`
  - `provider_events_institution_id_provider_provider_event_id_key`
  - `postings_account_idx`
- Journal totals equal posting totals.
- Stored balances equal reconstructed posting balances minus active holds.
- Migration runner reports `skip` on already-applied migrations.

### Compliance-Shaped Readiness

- Transfer, journal, posting, provider reference, status, and timestamp
  traceability must exist for every money movement.
- Manual-review deficits must be visible to operations/admin paths.
- Production gaps must remain explicit until implemented:
  real auth/RBAC, maker-checker, KYC/BVN, limits, fraud monitoring,
  reconciliation jobs, statements, regulatory returns, real provider signatures,
  and immutable audit trail.

## Reporting Requirements

Each run should update `docs/TEST_RESULTS.md` with:

- Date/time and timezone.
- Commit hash and dirty-worktree note.
- Exact commands run.
- Pass/fail table.
- Evidence snippets for important scenarios.
- Exact error output for failed scenarios.
- Handoff judgement.
- Smallest recommended fix for each issue.
- Untested or intentionally out-of-scope items.

## Cleanup Sweep

Do not remove code during a verification run. If unused or suspicious code is
found, cross-check it with at least two signals where possible, for example:

- `go test` / `go build` / `go vet` compiler-level checks.
- `staticcheck -checks=U1000` or equivalent unused-symbol analysis.
- `rg` references for the candidate symbol/table/path.

Record candidates in `docs/TEST_RESULTS.md` with evidence and defer cleanup to
a separate change.
