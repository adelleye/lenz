# Goal 11 — Mock Provider Name Enquiry

Add provider-shaped name enquiry using MockNIPProvider for Lenz Core Simple Transaction CBA v0.1.

## Branch

- Work on: `goal/cba-v0.1-11-mock-name-enquiry`
- Start from the latest reviewed/merged result of Goal 10.
- Do not work directly on `main`.
- Do not push directly to `main`.

## Objective

Add a production-shaped name enquiry endpoint backed only by `MockNIPProvider`.

This endpoint lets Lenz simulate the pre-transfer beneficiary lookup used by Nigerian bank-transfer flows.

This is not real NIBSS.
This is not real BankOne.
This is not real account validation with external institutions.

## Context

NIBSS/NIP-style transfers usually require beneficiary/account validation before transfer. The current repo already has a `TransferProvider` interface with `NameEnquiry`, and `MockNIPProvider` can simulate account-name lookup. Provider adapters must not post journals, mutate balances, write transaction history, or decide tenant scoping.

Public context You should check:

```text
NIBSS NIP:
https://nibss-plc.com.ng/nibss-instant-payment/

BankOne Transfer API:
https://docs.mybankone.com/transfers/transfer-api

Interswitch bank confirmation model, product-shape only:
https://docs.interswitchgroup.com/v1.1/docs/bank-confirmation-model-api
```

Use these as product-shape references only.

## Scope

Build:

- `POST /api/v1/external/name-enquiry`
- request/response schema
- service method
- handler method
- provider lookup using registered `TransferProvider`
- `MockNIPProvider` support if not already sufficient
- tests

Do not build:

- real NIBSS
- real BankOne
- real Interswitch
- real provider credentials
- external outbound transfer
- external inbound transfer
- TSQ/requery
- ledger posting
- holds
- fees
- cache
- UI

## Required API surface

Recommended route:

- `POST /api/v1/external/name-enquiry`

## Request fields

Minimum:

- `provider`: optional/default `mock_nip` or current mock provider name
- `bank_code` or `destination_institution_code`
- `account_number`
- `amount_minor`: optional, only if provider shape needs it
- `currency_id`: default `NGN`

Keep naming consistent with existing code style.

## Response fields

Minimum:

- `provider`
- `destination_institution_code`
- `account_number`
- `account_name`
- `provider_reference`, optional
- `status`: `found` | `not_found` | `provider_unavailable`
- `message`
- `created_at` if useful

Do not return sensitive internal account data.

## Behavior

Name enquiry must:

- require auth
- be institution-scoped
- call provider adapter only
- not create transfer records
- not create journal entries
- not create postings
- not change balances
- not create holds
- not write transaction history
- optionally write audit event `external_name_enquiry.performed` if audit exists and this is small

Recommended mock scenarios:

- valid account → found with account name
- unknown account → not_found
- provider timeout/unavailable → provider_unavailable
- invalid provider → controlled error

Do not silently fallback for money movement. Since name enquiry is read-only, fallback could be supported later, but do not build fallback routing in this goal.

## Provider registration

Use the existing provider registry/service pattern.

If `MockNIPProvider` is only wired when demo mode is enabled, decide and document one of these:

1. Keep name enquiry behind demo/mock mode for now.
2. Allow mock provider in local/test mode only.
3. Fail clearly if provider is not configured.

Do not silently create providers inside request handlers.

## OpenAPI rules

- `design/openapi/core/corebanking.yaml` remains source of truth.
- Add route and schemas.
- Use generated strict OpenAPI handlers.
- Regenerate with `go generate`.
- Do not manually edit generated `*.gen.go`.
- Do not commit generated `*.gen.go`.

## Repo rules

- Stay inside `apps/core/internal/corebanking`.
- Use `handler.go`, `service.go`, `provider.go`, `mock_nip_provider.go`.
- Add focused file only if helpful, e.g. `service_external.go`.
- Do not introduce new architecture.
- Do not introduce GORM.

## Validation

- institution from authenticated principal
- optional `X-Institution-ID` must match principal
- account_number required
- destination institution/bank code required
- currency if supplied must be `NGN`
- unsupported provider returns controlled error
- raw provider/internal errors are not exposed

## Tests

Add or update service tests:

1. valid mock name enquiry returns found account name.
2. unknown account returns not_found.
3. provider unavailable/timeout returns controlled provider_unavailable.
4. unsupported provider returns controlled error.
5. name enquiry does not create transfer.
6. name enquiry does not create journal/posting.
7. name enquiry does not change balance.
8. cross-tenant behavior is impossible because endpoint uses principal institution scope.

Add or update HTTP tests:

1. `POST /api/v1/external/name-enquiry` succeeds for valid mock scenario.
2. not_found scenario returns controlled response.
3. invalid provider returns 400 or equivalent.
4. missing auth rejected.
5. mismatched `X-Institution-ID` rejected.
6. invalid body returns 400.
7. response matches OpenAPI schema.

Add or update audit tests if audit is implemented for this event:

1. audit row exists.
2. metadata is sanitized.
3. audit row contains no Authorization/token/secrets.

## Manual/local verification

Use local Docker/Postgres only. Do not require real NIBSS/BankOne.

Actually verify:

1. start API with mock provider configured.
2. call name enquiry with a valid mock account.
3. call name enquiry with unknown account.
4. verify no transfer/journal/posting rows were created.
5. verify audit row only if implemented.

DB checks:

```sql
SELECT COUNT(*) FROM transfers;
SELECT COUNT(*) FROM journal_entries;
SELECT COUNT(*) FROM postings;
```

Run before and after name enquiry to prove no money movement occurred.

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
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/uat_simple_transaction_cba.sh
```

If UAT script does not exist, report clearly and run available tests/demo.

## Completion report

Report:

- branch name
- files changed
- endpoint added
- example request/response
- provider scenarios implemented
- DB evidence that no money moved
- audit status
- commands run and results
- deferred gaps
