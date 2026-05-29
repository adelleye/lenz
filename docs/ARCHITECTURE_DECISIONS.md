# Architecture Decisions

## Account Product Policy

Lenz Core keeps ordinary customer deposit accounts non-negative. Internal
clearing/settlement accounts may carry operational negative balances.

| Account/product type | Allows normal negative customer balance? | Notes |
| --- | --- | --- |
| `standard_wallet` | No | Default demo customer account. Customer-initiated spend must be limited by available balance. |
| `standard_current` | No | Same spend rule as wallet unless explicitly migrated to a credit product. |
| `standard_savings` | No | Same spend rule as wallet. |
| `internal` | Yes, operationally | Internal clearing/settlement accounts can represent system positions and are not customer spend products. |

The code stores this as account policy fields on `accounts`:

- `product_type`
- `allow_negative_balance`

The default for customer accounts is `allow_negative_balance = false`.

## Ledger Balance vs Available Balance

`ledger_minor` is the posted accounting balance derived from immutable
double-entry postings.

`available_minor` is the spendable balance cache:

```text
available_minor = ledger_minor - active outbound holds
```

For standard wallet/current/savings accounts, customer-initiated outbound
transfers must not proceed when `available_minor` is below the transfer amount.
For explicitly configured overdraft/credit accounts, negative available and
ledger balances can be normal product behavior.

`account_balances` remains a speed cache. Ledger correctness is still proven
from `postings`; available balance must reconcile to posted ledger balance minus
active holds.

## Holds and Reservations

Pending outbound transfers create an `account_holds` row with `status = active`.
The hold reduces `available_minor` immediately and does not change
`ledger_minor`.

When the provider later fails the transfer:

- the transfer is marked failed,
- the hold is released,
- `available_minor` is restored,
- no journal entry or posting is created.

When the provider later succeeds:

- a journal entry and postings are created,
- `ledger_minor` is moved,
- the hold is consumed,
- `available_minor` does not get debited twice.

Pending inbound transfers are recorded in transfer history, but they do not
increase available balance or ledger balance until provider success.

## External Requery Policy

External requery is an explicit operator/API action, not background polling in
this prototype.

`POST /api/v1/external/transfers/{transfer_id}/requery` only applies to
external transfers that are still `pending` or `provider_unknown`. Already-final
transfers are deterministic no-ops, and internal transfers are rejected/no-op at
this boundary.

Requery outcomes follow the same ledger rules as settlement:

- outbound success consumes the existing hold and posts exactly one journal;
- outbound failure releases the hold and posts no journal;
- outbound still-pending or provider-unknown keeps the hold active for later
  reconciliation;
- inbound success credits the destination account once;
- inbound pending, provider-unknown, or failed outcomes do not credit money.

Unknown provider outcomes must not silently fall back to another provider. They
must remain visible in transfer and reconciliation views.

## Reversal Deficit Policy

Reversals always create new ledger events. Lenz Core must never delete or edit
the original ledger history to hide a reversal.

If reversing a prior inbound transfer would push a standard customer account
below zero, the reversal is still posted as a new journal entry, but it is not
classified as normal overdraft. The transfer is marked:

- `ledger_status = reversal_deficit`
- `reconciliation_status = manual_review`

Operations must treat that balance as a customer receivable/manual-review case.
The resulting deficit is not spendable balance; further standard outbound spend
still fails unless the account is explicitly configured as a credit product.

## Overdraft as a Separate Future Product

Overdraft is not a flag to turn on for all customer accounts. It must be a
separate product policy with explicit account configuration, pricing, limits,
terms, disclosures, and operational monitoring.

This pass only makes the representation possible with `allow_negative_balance`.
It does not implement credit limits, interest, fees, collections, statements, or
regulatory disclosures.

## Provider Status, Ledger Status, Reconciliation Status

Transfers carry three separate status concerns:

- `provider_status`: provider-side state mapped into `pending`, `succeeded`, or
  `failed`.
- `ledger_status`: Lenz ledger state, such as `pending`, `posted`,
  `no_posting`, or `reversal_deficit`.
- `reconciliation_status`: operational state, such as `pending`, `matched`,
  `no_action`, or `manual_review`.

The legacy `status` field remains as the API-level transfer summary for the
current demo.

## Mock Behavior vs Production Behavior

The mock provider routes and mock external endpoints are proof tools, not
production transfer rails. They can simulate immediate success, pending,
failure, timeout/provider-unknown outcomes, settlement, requery, and duplicate
provider events, but they do not prove settlement finality with a real scheme or
bank.

Production provider integration must reserve funds before sending a real
outbound instruction. If a provider reports success after Lenz has rejected a
transfer for policy reasons, that is an operational incident, not a normal
happy path.

Demo seed data and mock routes must stay clearly demo-scoped and must not be
exposed as production-ready money movement APIs.

The HTTP demo seed/mock routes are only registered when `LENZ_DEMO_MODE=true`.
All non-health routes require a bearer token; the current `LENZ_DEV_AUTH_TOKEN`
path is a fail-closed local development control, not the final production
authentication model.

## Before Real Provider Integration

Before adding Monnify, Interswitch, NIBSS, Providus, BankOne, or sponsor-bank
integrations, these must be true:

- every customer money movement has institution scope,
- every outbound spend checks available balance under account policy,
- pending outbound transfers reserve available balance,
- failed pending outbound transfers release holds without postings,
- successful pending outbound transfers consume holds and post immutable
  double-entry journals,
- pending inbound transfers do not affect balances,
- duplicate idempotency keys and duplicate provider events do not double-post,
- ledger balances reconcile to postings,
- available balances reconcile to ledger minus active holds,
- reversal deficits are visible for operations/manual review,
- demo/mock endpoints cannot be mistaken for production provider APIs,
- provider status, ledger status, and reconciliation status are not collapsed
  into one ambiguous field.
