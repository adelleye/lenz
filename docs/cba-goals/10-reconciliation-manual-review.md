# Goal 10 — Reconciliation and Manual-Review Queue

Add a minimal reconciliation/manual-review queue for Lenz Core Simple Transaction CBA v0.1.

## Branch

- Work on: `goal/cba-v0.1-10-reconciliation-manual-review`
- Start from the latest reviewed/merged result of Goal 09 plus hardening fixes.
- Do not work directly on `main`.
- Do not push directly to `main`.

## Objective

Add a simple operational queue that lets admins/operators see transfers and ledger situations that need review.

This goal is not full settlement reconciliation. It is the minimum queue needed before adding provider-shaped external flows.

## Context

Previous goals should have added:

- customer creation
- account creation
- balance enquiry
- internal credit
- internal debit
- internal transfer
- transaction history
- account controls
- audit events
- idempotency mismatch protection
- provider-event mismatch protection
- dev-auth production guardrail if implemented

Current transfer records already have separate state concepts:

- `provider_status`
- `ledger_status`
- `reconciliation_status`

Do not collapse them.

Public context:

```text
NIBSS NIP:
https://nibss-plc.com.ng/nibss-instant-payment/

BankOne Transfer API:
https://docs.mybankone.com/transfers/transfer-api

BankOne Transaction Status Confirmation:
https://docs.mybankone.com/transactions/transactions-api/transaction-status-confirmation

CBN MFB Monthly Returns:
https://www.cbn.gov.ng/supervision/mfbreturns.html
```

Use these as product-shape context only. Do not implement real NIBSS or BankOne.

## Scope

Build:

- admin/manual-review list endpoint
- detail endpoint for a single reconciliation item, if useful
- service/repository query logic
- status filters
- pagination
- tests proving the queue shows the right exceptions
- audit event when an item is marked reviewed/resolved, if audit infrastructure exists

Do not build:

- full automated settlement matching
- file upload
- NIBSS settlement report parser
- BankOne report parser
- real provider integration
- accounting period close
- regulatory returns
- full operations UI
- maker-checker
- fraud engine

## Required API surface

Recommended routes:

- `GET /api/v1/admin/reconciliation-items`
- `GET /api/v1/admin/reconciliation-items/{transfer_id}`
- `POST /api/v1/admin/reconciliation-items/{transfer_id}/mark-reviewed`

If the repo already uses a different admin route style, stay consistent.

## What should appear in the queue

Include transfers/items with any of these states:

1. `reconciliation_status = manual_review`
2. `provider_status = provider_unknown`
3. `ledger_status = reversal_deficit`
4. `provider_status = succeeded` and `ledger_status != posted`
5. `ledger_status = posted` and `provider_status = failed`
6. pending provider transfer older than a configurable/test threshold
7. duplicate/mismatched provider event flagged by existing provider-event protection
8. idempotency mismatch conflict if it is stored as a reviewable event
9. external inbound event that could not resolve destination account
10. external inbound event that was rejected because of material payload mismatch

Keep it simple. If some of these states do not exist yet, support the ones that exist and document the rest as deferred.

## Query parameters

Support:

- `limit`
- `before_created_at` or existing cursor
- `status` / `reconciliation_status`
- `provider_status`
- `ledger_status`

Keep filters simple and bounded.

Default limit: use existing repo default if present, otherwise 100.  
Max limit: use existing cap if present, otherwise 200.

## Response fields

Minimum per item:

- `transfer_id`
- `institution_id`
- `account_id`
- `direction`
- `amount_minor`
- `currency_id`
- `provider`
- `provider_reference`
- `provider_event_id`
- `provider_status`
- `ledger_status`
- `reconciliation_status`
- `failure_reason`
- `journal_entry_id`
- `created_at`
- `updated_at`
- `review_reason`
- `recommended_next_action`

`recommended_next_action` can be simple strings such as:

- `requery_provider`
- `inspect_journal`
- `contact_provider`
- `manual_customer_receivable_review`
- `no_action`

Do not overbuild a workflow engine.

## Mark reviewed behavior

`POST /api/v1/admin/reconciliation-items/{transfer_id}/mark-reviewed`

Request:

- `resolution_note`: required
- `resolution_status`: allowed values: `reviewed`, `resolved_no_action`, `manual_followup_required`

Behavior:

- institution-scoped
- must only update reconciliation metadata/status
- must not mutate journal entries
- must not mutate postings
- must not mutate customer balances
- must write an audit event if audit is available
- must preserve original transfer history

Do not allow this endpoint to “fix money” directly. It only records operational review.

## OpenAPI rules

- `design/openapi/core/corebanking.yaml` remains the source of truth.
- Add schemas and routes there.
- Use generated strict OpenAPI handlers.
- Run `go generate`.
- Do not manually edit generated `*.gen.go`.
- Do not commit generated `*.gen.go`.

## Repo rules

- Stay inside `apps/core/internal/corebanking`.
- Use `model.go`, `repository.go`, `service.go`, `handler.go`.
- Add focused SQL file if needed, e.g. `sql_reconciliation_repository.go`.
- Use `sqlx`.
- Use explicit SQL transactions only where mutations happen.
- Do not introduce GORM.
- Do not introduce a new architecture.

## Validation

- institution comes from authenticated principal
- optional `X-Institution-ID` must match principal institution
- cross-institution items must not be visible
- invalid cursor/filter returns controlled error
- raw SQL/internal errors are not exposed
- mark-reviewed requires a note
- mark-reviewed cannot alter ledger/postings/balances

## Tests

Add or update service tests:

1. manual-review transfer appears in queue.
2. provider_unknown transfer appears in queue.
3. reversal_deficit transfer appears in queue if supported.
4. normal matched/posted/succeeded transfer does not appear.
5. queue filters by provider_status.
6. queue filters by ledger_status.
7. queue filters by reconciliation_status.
8. cross-institution items are hidden.
9. mark-reviewed changes only reconciliation/review metadata.
10. mark-reviewed writes audit event if audit exists.
11. mark-reviewed does not alter balance/postings/journal.

Add or update Postgres integration tests:

1. create normal posted transfer and assert absent from queue.
2. create provider_unknown transfer and assert present.
3. create manual_review transfer and assert present.
4. create reversal_deficit transfer and assert present if existing reversal logic supports it.
5. mark item reviewed.
6. assert audit row exists if audit is available.
7. assert journal/postings/balances unchanged after review.
8. assert pagination works without duplicates.

Add or update HTTP tests:

1. `GET /api/v1/admin/reconciliation-items` returns expected items.
2. filters work.
3. empty result returns `[]`, not `null`.
4. `GET /api/v1/admin/reconciliation-items/{transfer_id}` returns detail.
5. mark-reviewed works.
6. missing auth rejected.
7. mismatched `X-Institution-ID` rejected.
8. cross-tenant access rejected.
9. invalid request returns 400.

## Manual/local verification

Use local Docker/Postgres only. Do not require real NIBSS/BankOne.

Actually verify:

1. bootstrap local CBA.
2. create customer/account.
3. create normal internal credit/debit and confirm not in queue.
4. create or seed provider_unknown transfer and confirm it appears.
5. create or seed manual_review transfer and confirm it appears.
6. mark item reviewed.
7. query audit_events.
8. check balances/journals unchanged.

DB checks:

```sql
SELECT id, provider_status, ledger_status, reconciliation_status
FROM transfers
ORDER BY created_at DESC;

SELECT action, entity_type, entity_id, transfer_id
FROM audit_events
ORDER BY created_at DESC
LIMIT 20;
```

## Required commands

Run:

```sh
go generate ./apps/core/internal/corebanking
go generate ./apps/core/internal/institution
git check-ignore -v apps/core/internal/corebanking/corebanking.gen.go
git check-ignore -v apps/core/internal/institution/institution.gen.go
go test -race -count=1 ./apps/core/internal/corebanking
go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
go build ./apps/core/... ./apps/auth/... ./packages/shared/...
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/demo_transfer_spine.sh
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/bootstrap_cba_v0_1.sh
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/uat_simple_transaction_cba.sh
```

If bootstrap/UAT scripts do not exist in the current branch, report that clearly and run the available demo script/tests.

## Completion report

Report:

- branch name
- files changed
- endpoints added
- migrations added, if any
- example queue response
- mark-reviewed example
- audit evidence
- DB evidence that balances/journals were not mutated
- commands run and results
- deferred gaps
