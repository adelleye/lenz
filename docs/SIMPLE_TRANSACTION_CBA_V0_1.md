# Simple Transaction CBA v0.1

This document defines the current product slice in plain terms.

Lenz Core v0.1 is a multi-tenant, ledger-first transaction core for Nigerian
MFB-style accounts. It supports basic customer/account operations, internal
money movement, mock external transfer lifecycle behavior, audit events, and
reconciliation/manual-review visibility.

It is compliance-shaped, not CBN-compliant production software.

## What v0.1 Includes

- customer creation;
- account creation with supplied unique 10-digit test account numbers;
- balance enquiry;
- internal credit;
- internal debit;
- internal account-to-account transfer;
- ledger-backed transaction history;
- account controls: freeze, post-no-debit, and liens;
- audit events;
- reconciliation/manual-review queue;
- mock name enquiry;
- mock external outbound transfer lifecycle;
- mock external inbound event lifecycle;
- mock transaction status query/requery.

## What v0.1 Excludes

- real NIBSS, BankOne, Monnify, Interswitch, Providus, or sponsor-bank rails;
- production auth/RBAC;
- maker-checker;
- KYC/BVN/NIN verification;
- true NUBAN issuance/check-digit generation. Full NUBAN generation/check-digit validation is deferred;
- limits and fraud monitoring;
- loans, cards, fees, interest, statements, regulatory returns;
- frontend/mobile apps;
- cloud deployment.

## Current Repo Position

The repo proves the transaction spine locally with Go, Postgres, OpenAPI, and a
mock provider.

It already proves:

- posted money movement uses balanced journal entries and postings;
- account balances split ledger balance from available balance;
- pending outbound transfers reserve funds with holds;
- failed pending outbound transfers release holds without postings;
- successful pending outbound transfers consume holds and post once;
- duplicate idempotency keys and provider events do not double-post;
- inbound success credits once;
- provider-unknown and manual-review cases remain visible;
- reversals create new ledger events;
- generated strict OpenAPI handlers are used;
- local Docker/Postgres proof scripts pass.

## Principles To Preserve

- Money movement is not a direct balance update.
- `journal_entries` and `postings` are the ledger source of truth.
- `account_balances` is a read/cache table.
- Standard customer accounts cannot casually go negative.
- External transfers are state machines, not single API calls.
- Unknown provider outcomes must not silently fall back to another provider.
- Every money action must be idempotent and institution-scoped.
- Exceptions must be visible in reconciliation/manual review.
- Generated `*.gen.go` files are not edited manually.

## Build Sequence

Completed:

- Builds 1-7: customers, accounts, balance enquiry, internal money movement,
  and transaction history.
- Build 8: account controls.
- Build 9: audit events.
- Build 10: reconciliation/manual-review queue.
- Build 11: mock name enquiry.
- Build 12: mock external outbound lifecycle.
- Build 13: mock external inbound lifecycle.
- Build 14: mock transaction status query/requery.

Blocked until provider access and production design:

- Build 15: real provider adapter.

## Verification

Required local checks:

```sh
go generate ./apps/core/internal/corebanking
git check-ignore -v apps/core/internal/corebanking/corebanking.gen.go
go test -race -count=1 ./apps/core/internal/corebanking
go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...
go build ./apps/core/... ./apps/auth/... ./packages/shared/...
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/uat_simple_transaction_cba.sh
TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/demo_transfer_spine.sh
```

Real provider verification is blocked until real sandbox/API access exists.
