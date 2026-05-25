# Lenz Core Simple Transaction CBA v0.1

## Product Definition

Lenz Core Simple Transaction CBA v0.1 is a multi-tenant, ledger-first transaction core for Nigerian MFB-style accounts that safely supports internal and external money movement, accurate balances, transaction history, audit events, and reconciliation/manual-review states.

This is not a claim of CBN compliance. v0.1 is compliance-shaped and designed with Nigerian MFB/payment constraints in mind. Production compliance still needs licensed-MFB compliance/legal review, provider certification, NIBSS or sponsor-bank onboarding, real auth/RBAC, real audit controls, and operating procedures.

## Current Repo Position

The current repo is a verified transaction-spine prototype, not the production CBA architecture. It already proves:

- ledger-first money movement through balanced journal entries and postings;
- ledger and available balance separation;
- pending outbound holds;
- idempotency key and provider event duplicate protection;
- transfer history from Lenz records;
- reversal as a new ledger event;
- reversal deficit/manual-review classification;
- generated strict OpenAPI handlers from `design/openapi/core/corebanking.yaml`;
- local Docker/Postgres proof through `scripts/demo_transfer_spine.sh`.

The current production gap is that most behavior is still demo/mock shaped. Real customer/account creation, account controls, audit writes, reconciliation queues, and real provider rails are not implemented yet.

## Repo Conventions To Preserve

v0.1 should continue to use:

- Go;
- Postgres;
- `sqlx`;
- `chi`;
- `oapi-codegen`;
- generated strict OpenAPI handlers;
- `design/openapi/core/corebanking.yaml` as the HTTP source of truth;
- `apps/core/internal/corebanking` module structure;
- `server.Deps` wiring through `deps.Cfg.DBConn`.

Do not manually edit generated `*.gen.go` files. Do not introduce GORM. Do not bypass the generated strict handler path.

## Public Context Checked

The expectation below was checked against these public sources:

- CBN BVN page: https://www.cbn.gov.ng/PaymentsSystem/BVN.html
- CBN Microfinance Monthly Returns Guide: https://www.cbn.gov.ng/supervision/mfbreturns.html
- NIBSS NIP public page: https://nibss-plc.com.ng/nibss-instant-payment/
- NIBSS NIP services article: https://contactcentre.nibss-plc.com.ng/support/solutions/articles/47001265115-what-are-the-services-under-nip-that-customers-can-utilize-
- CBN revised NUBAN standard: https://www.cbn.gov.ng/Out/2020/PSMD/REVISED%20STANDARDS%20ON%20NIGERIA%20UNIFORM%20BANK%20ACCOUNT%20NUMBER%20%28NUBAN%29%20FOR%20BANKS%20AND%20OTHER%20FINANCIAL%20INSTITUTIONS%20.pdf
- BankOne account API docs: https://docs.mybankone.com/account/account-api
- BankOne transfer API docs: https://docs.mybankone.com/transfers/transfer-api

Public-source verification status: checked.

Important constraints to carry into v0.1:

- CBN public BVN guidance says individual Tier 2 and Tier 3 accounts/wallets require BVN and NIN, while Tier 1 accounts/wallets require either BVN or NIN. v0.1 should not integrate BVN/NIN APIs yet, but customer/account models must leave room for identity status and later electronic verification.
- CBN public MFB returns guidance says monthly returns are a regulatory requirement for MFBs. v0.1 should not implement regulatory returns yet, but customer, account, transaction, audit, and reconciliation data must be durable and reportable later.
- NIBSS NIP public material describes name enquiry, funds transfer direct credit, funds transfer direct debit, transaction status query, and balance enquiry. v0.1 should model provider-shaped lifecycle behavior before real NIBSS integration.
- NIBSS describes NIP settlement as deferred net settlement where beneficiary value can be available online real-time before settlement. v0.1 must keep `provider_status`, `ledger_status`, and `reconciliation_status` separate.
- CBN NUBAN standards define customer account numbers as 10 digits unique within each deposit-taking institution, with check digit validation. v0.1 should document this as a production account-number requirement; do not implement it unless a later build explicitly scopes it.
- BankOne account docs include account creation, account enquiry, freeze/unfreeze, lien, post-no-debit, BVN details, balance enquiry, statements, transaction search, and transactions.
- BankOne transfer docs include name enquiry and intra-bank fund transfer flows.

## Scope

v0.1 includes:

- customers;
- accounts;
- balance enquiry;
- internal credit;
- internal debit;
- internal account-to-account transfer;
- transaction history;
- account controls later in v0.1;
- audit events;
- reconciliation/manual-review queue;
- mock provider-shaped external transfers before real provider integration.

## Non-Scope

v0.1 excludes:

- loans;
- interest;
- cards;
- full fees engine;
- real NIBSS;
- real BankOne;
- real Monnify;
- real Interswitch;
- real Providus;
- full regulatory returns;
- production auth/RBAC;
- full maker-checker;
- frontend;
- mobile app;
- cloud deployment.

## First Principles

- Money movement is not a direct balance update.
- Every posted money movement must have balanced journal postings.
- `account_balances` is a speed cache, not the source of truth.
- Standard customer accounts must not casually go negative.
- Negative balances are allowed only for internal accounts, explicit future overdraft/credit products, or reversal-deficit/manual-review cases.
- External transfers are state machines, not simple API calls.
- Do not blindly fallback for money movement when provider status is unknown.
- Every money action must be idempotent.
- Every money-sensitive action must be institution-scoped.
- Every money-sensitive action must eventually write an audit event.
- Exceptions must be visible in reconciliation/manual review.

## Build Sequence

Build 0: expectation document.

Build 1: real customer creation.

Build 2: real account creation.

Build 3: balance enquiry hardening.

Build 4: internal credit.

Build 5: internal debit.

Build 6: internal account-to-account transfer.

Build 7: ledger-backed transaction history.

Build 8: account controls: freeze, PND, lien.

Build 9: audit events for all money actions.

Build 10: reconciliation/manual-review queue.

Build 11: mock name enquiry.

Build 12: mock external outbound transfer lifecycle.

Build 13: mock external inbound event lifecycle.

Build 14: mock transaction status query/requery.

Build 15: real provider adapter, blocked until the team has provider/sandbox/API access.

## Acceptance Criteria For v0.1

- Institution can create customer.
- Institution can create account.
- Standard customer account cannot spend below available balance.
- Internal credit posts balanced journal and increases balance.
- Internal debit posts balanced journal and decreases balance.
- Internal transfer debits one account and credits another.
- Transaction history shows posted, pending, failed, and reversed movements.
- Duplicate idempotency keys do not double-post.
- Duplicate provider events do not double-post.
- Pending outbound creates hold.
- Failed pending releases hold.
- Successful pending consumes hold and posts journal.
- External inbound success credits once.
- `provider_unknown` and `manual_review` states are visible.
- Reversals create new ledger events.
- Reversal deficits are manual-review cases, not casual overdrafts.
- Audit events are written for money-sensitive actions.
- Reconciliation/admin views show exceptions.
- All money operations are institution-scoped.
- Demo/mock routes cannot be mistaken for production routes.

## Verification Strategy

No paid cloud database is needed for these builds. Use local Docker/Postgres/Redis and Go tests.

Required checks for each build:

- `go generate ./apps/core/internal/corebanking`
- `go generate ./apps/core/internal/institution`
- `git check-ignore -v apps/core/internal/corebanking/corebanking.gen.go`
- `git check-ignore -v apps/core/internal/institution/institution.gen.go`
- `go test -count=1 ./apps/core/... ./apps/auth/... ./packages/shared/...`
- `go build ./apps/core/... ./apps/auth/... ./packages/shared/...`

Use `TMPDIR=$PWD/tmp POSTGRES_PORT=55432 ./scripts/demo_transfer_spine.sh` when the build touches money movement or when local Docker is available.

Real provider verification is blocked until provider sandbox/API access exists. Mock provider tests should simulate provider success, failure, pending, timeout, duplicate webhook, and delayed status.

## Legal And Compliance Review

These items are intentionally left as needs legal/compliance review:

- whether a deploying institution is licensed and permitted to run the product;
- final KYC tier rules and BVN/NIN operational process;
- NIBSS, sponsor-bank, or provider certification requirements;
- monthly returns content and submission process;
- audit retention and immutability controls;
- maker-checker policy for money-sensitive actions;
- customer disclosures, fees, overdraft/credit terms, and complaints handling.

## Build 0 Completion Boundary

Build 0 is complete when this document exists, no code/migration/OpenAPI/provider changes are included, generated files remain ignored, and the existing code still generates, tests, and builds.
