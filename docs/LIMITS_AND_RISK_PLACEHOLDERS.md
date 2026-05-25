# Limits And Risk Placeholders

This document is a v0.1 placeholder only. It does not implement rate limiting,
fraud rules, compliance limits, or production risk controls.

The current Simple Transaction CBA v0.1 proves the transaction spine: customers,
accounts, balances, internal money movement, account controls, audit events, and
ledger reconciliation. Production use will still need explicit limits and risk
controls before real customer money is handled.

## Deferred Limit Controls

The following controls are deferred:

- per-user transaction and API limits;
- per-account debit and transfer limits;
- per-institution operating limits;
- daily debit limits;
- single-transfer amount limits;
- velocity checks for repeated transfers, debits, credits, failed attempts, and
  account-control actions;
- fraud/manual-review thresholds for unusual value, frequency, counterparties,
  reversal deficits, or repeated failed attempts;
- API rate limiting for authenticated non-health routes.

## Ownership

Actual limits must be defined by the licensed MFB, compliance, risk, operations,
and product teams. This repo should not hard-code CBN, OFI, sponsor-bank, or
internal policy limits without an approved implementation scope and source of
truth.

## Non-Implementation Statement

This placeholder intentionally does not add:

- HTTP middleware;
- database tables;
- policy engines;
- fraud scoring;
- CBN-limit assumptions;
- provider-rail rules;
- maker-checker workflows;
- external risk services.

When these controls are scoped later, they should be added with migrations,
tests, audit events, operational documentation, and clear ownership of limit
configuration.
