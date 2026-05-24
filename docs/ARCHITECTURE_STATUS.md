# Architecture Status

## Corebanking HTTP Architecture

Corebanking routes now follow the same OpenAPI-first direction as the
institution module:

- `design/openapi/core/corebanking.yaml` is the source of truth for the current
  transaction-spine HTTP surface.
- `apps/core/internal/corebanking/config.yaml` configures `oapi-codegen` for
  chi server routes, models, strict-server types, and the embedded spec.
- `apps/core/internal/corebanking/doc.go` wires `go generate` to regenerate the
  corebanking server stubs.
- `apps/core/internal/corebanking/corebanking.gen.go` is generated code and is
  intentionally ignored by git.
- `apps/core/internal/institution/institution.gen.go` is generated from
  `design/openapi/core/institution.yaml` and is intentionally ignored by git.

The hand-written HTTP server is intentionally thin. It satisfies the generated
`ServerInterface`, lets generated routing bind path and header parameters, uses
`chi/render` binding for JSON request bodies that still need manual decoding,
and calls the service only after request validation succeeds.

## Repository Layer

The service depends on repository interfaces in `repository.go`, not direct SQL.
The SQL implementation is split by concern:

- `sql_account_repository.go` handles account, balance, and transaction-history
  reads.
- `sql_ledger_repository.go` handles journal/posting reads and posting writes.
- `sql_transfer_repository.go` handles transfer creation, lookup, settlement,
  idempotency, and reversal orchestration.
- `sql_holds.go` handles pending outbound hold creation, release, and
  consumption.
- `sql_provider_event_repository.go` handles duplicate provider-event
  protection and transfer linking.
- `sql_demo_repository.go` keeps demo seed writes separate from money movement.

`NewRepository` is the constructor used by `apps/core/main.go` through the
existing `server.Deps` wiring.

## Transaction Helper

`WithTx(ctx, db, func(tx TxRunner) error)` is the reusable transaction wrapper.
It begins a `sqlx.Tx`, runs the callback, commits on nil error, and rolls back
on callback or commit failure. Core money-moving operations use this helper so
ledger postings, balance cache updates, holds, transfers, and provider-event
links remain atomic.

The integration test suite verifies both commit and rollback behavior around
money movement.

## Provider Shape

Provider abstractions now separate the external institution/provider identity
from transfer-specific capability:

- `Provider` names the external provider.
- `TransferProvider` embeds `Provider` and adds name enquiry, transfer
  initiation, transfer requery, and webhook parsing.

`MockNIPProvider` remains the only implemented provider. It is still a demo
adapter for transfer-spine proof flows and is not a real Monnify, Interswitch,
NIBSS, Providus, BankOne, SkyPay, MFB, or sponsor-bank integration.

Fallback-provider scaffolding is not part of the current code path. Future
provider work should be a production-shaped slice with explicit credentials,
signed webhooks, requery, and reconciliation rules.

## Mock/Demo vs Production-Shaped

Mock/demo:

- `POST /api/v1/demo/seed`
- `POST /api/v1/transfers/mock/inbound`
- `POST /api/v1/transfers/mock/outbound`
- `MockNIPProvider`

Those routes are still registered for local proof flows only when
`LENZ_DEMO_MODE=true`. All non-health routes remain protected by the existing
development bearer-token gate.

Production-shaped:

- institution-scoped reads and writes,
- idempotency keys,
- duplicate provider-event protection,
- double-entry journal postings,
- ledger balance vs available balance separation,
- pending outbound holds,
- hold release/consumption on provider settlement,
- reversal-as-new-ledger-event behavior,
- reversal deficit/manual-review classification.

## Before Real SkyPay/MFB Use

Before real SkyPay, MFB, sponsor-bank, or scheme use, Lenz Core still needs:

- real authn/authz and tenant/user role enforcement instead of the development
  bearer token;
- production provider adapters and signed webhook verification;
- maker-checker and limit checks for sensitive transfers and reversals;
- reconciliation jobs and provider requery workers;
- operational audit/event trails around every money-control decision;
- provider-specific failure mapping and settlement finality handling;
- account-product configuration for overdrafts, fees, statements, reports, and
  regulatory disclosures;
- CI enforcement that generated OpenAPI stubs stay current with the spec.
