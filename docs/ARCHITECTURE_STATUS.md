# Architecture Status

This document explains the current shape of the system. It is not a future
architecture proposal.

## Current Slice

Lenz Core currently implements the Simple Transaction CBA v0.1 transaction
spine:

- customers and accounts;
- account balances split into ledger and available balance;
- internal credits, debits, and transfers;
- account controls: freeze, post-no-debit, and liens;
- audit events;
- reconciliation/manual-review views;
- mock external name enquiry, outbound transfer, inbound event ingestion, and
  transfer requery.

The external paths are provider-shaped mock flows only. They do not connect to
real NIBSS, BankOne, Monnify, Interswitch, Providus, or a sponsor bank.

## Request Path

The main request path is:

```text
design/openapi/core/corebanking.yaml
  -> go generate
  -> generated strict handler/types
  -> apps/core/internal/corebanking/handler*.go
  -> service*.go
  -> repository.go interfaces
  -> sql_*.go Postgres implementations
```

Generated files are ignored by git and should not be edited manually.
`design/openapi/core/institution.yaml` is a no-route placeholder for a future
institution module; it is generated to keep that path explicit but does not
define any active runtime route today.

## Module Boundaries

`apps/core/server` owns server setup:

- auth middleware;
- institution-scope checks;
- CORS;
- demo-mode gates;
- route registration;
- dependency wiring through `server.Deps`.

`apps/core/internal/corebanking/handler*.go` owns the HTTP boundary:

- generated strict-handler methods;
- request validation/adaptation;
- response shaping;
- mapping service errors to HTTP responses.

`service*.go` owns banking decisions:

- account policy;
- balance availability;
- holds;
- ledger posting decisions;
- provider status handling;
- reconciliation status;
- audit writes.

`sql_*.go` owns persistence:

- SQL transactions;
- idempotency constraints;
- provider-event duplicate protection;
- journal/posting writes;
- balance and hold updates;
- list/read queries.

## Provider Boundary

Provider adapters return provider-shaped results. They do not post journals,
mutate balances, decide tenant scope, release holds, or mark reconciliation
items reviewed.

The current adapter is `MockNIPProvider`. It supports mock name enquiry,
outbound initiation, inbound webhook/event parsing, and transfer requery. It is
a proof adapter, not a real NIP integration.

Important rule: unknown provider outcomes do not fall back to another provider.
They remain visible as pending or provider-unknown transfers for reconciliation
or explicit requery.

## Requery Behavior

`POST /api/v1/external/transfers/{transfer_id}/requery` only acts on external
transfers that are still `pending` or `provider_unknown`.

- Outbound success consumes the hold and posts one journal.
- Outbound failure releases the hold and posts no journal.
- Still-pending or provider-unknown keeps the hold and remains visible for
  reconciliation.
- Inbound success credits once.
- Already-final transfers are deterministic no-ops.
- Internal transfers are rejected/no-op at this boundary.

Concurrent requery calls are expected to converge on one money effect without
500 responses for legitimate duplicate calls.

## Production Gaps

Before production use, the system still needs:

- real auth/RBAC and tenant/user role enforcement;
- maker-checker and limit checks;
- KYC/BVN/NIN verification;
- true NUBAN issuance/check-digit validation;
- real provider adapters and signed webhooks;
- provider settlement/reconciliation operations;
- monitoring, alerting, and deployment hardening;
- compliance reporting and operating procedures.
