
# Lenz Core — Product Requirements Document (PRD)
*A next‑gen, multi‑tenant Core Banking Application for Nigeria (and beyond)*

**Owner:** Clive Alliance — Lenz Team  
**Product Name:** Lenz Core  
**Version:** v1.0 (PRD)  
**Last Updated:** 2025‑12‑03

---

## 0) Executive Summary
Lenz Core is a **bank‑grade, multi‑tenant Core Banking Application (CBA)** with a React **Vite** TypeScript frontend and a Golang backend using **GORM** + PostgreSQL. It is designed so **licensed institutions** (MFBs, commercial, digital banks) can create tenant profiles, set up **branches** and **sub‑banks**, operate **accounts, transfers, fees, loans, and reporting**, and enforce **bank‑grade security** (MFA/2FA, encryption in transit/at‑rest, audit logs, maker–checker).

Lenz Core integrates the **Lenz AI** advisor for **insights** and **loan approval assistance**. It’s shipped as a **monorepo** with a tenant‑aware frontend (white‑label theming via subdomains) and a high‑throughput backend built for correctness, speed, and Nigerian operations (NUBAN, BVN/ICAD hooks, NIP adapters via bank credentials).

---

## 1) Goals & Non‑Negotiables
- **Security first:** MFA/2FA required for sign‑in and **all high‑risk actions**; end‑to‑end TLS; encryption at rest with **external key management**. Full audit trails + immutability.
- **Correctness:** **Double‑entry** ledger with atomic postings; idempotency across all money‑moving endpoints; period locks; maker–checker.
- **Nigeria‑ready:** NUBAN, BVN capture, tenant‑per‑bank; NIBSS/NIP adapter module (credentials per bank); SMS (Termii) + Email (SendGrid).
- **Multi‑tenant SaaS:** One deployment serves many banks; strict tenant isolation; bank‑level branding via subdomains.
- **Full lending:** **All loan types** (personal/SME/micro/mortgage/overdraft/revolving); schedules; accruals; delinquency; restructuring; (credit scoring later).
- **Operational excellence:** EOD close; GL export; disputes & reversals; reconciliation; reports; observability; SLOs.
- **DX:** Monorepo; Postman collection; OpenAPI; robust README; seed data; Docker Compose.

**Out‑of‑scope v1:** Cards issuing/processing, FX trading, full credit bureau integrations (placeholders allowed).

---

## 2) Users & Tenancy Model
### 2.1 User Types
- **Retail/SME Customer:** Self‑service banking (balances, transfers, statements, loan apps, insights).
- **Bank Staff:** Teller, CS agent, Loan officer, Branch manager, Compliance/Audit, Finance, Admin.
- **Super Admin (Platform):** Lenz ops (no access to PII/transactions — meta/monitoring only).

### 2.2 Tenancy
- **Row‑level multi‑tenancy** (shared DB): every row has `tenant_id`. Composite uniques include `tenant_id` (e.g., account_number). All queries MUST scope by `tenant_id`.
- **Branch + Sub‑Bank:** `institutions` (tenants) may create **branches** (physical locations) and **sub‑banks** (affiliates that settle to parent). Permissions scope by org unit.
- **Branding by subdomain:** `https://{bank}.lenzcore.app` loads tenant theme (logo, colors), feature flags, email/SMS templates.

---

## 3) Functional Requirements
### 3.1 Onboarding & KYC
- Customer record with PII, BVN, documents; status: `PENDING_KYC | ACTIVE | FROZEN | CLOSED`.
- Staff invite/login with MFA; granular RBAC; device/session mgmt.
- Institution setup wizard: branding, products, fees, limits, branches, sub‑banks, Termii/SendGrid keys.

### 3.2 Ledger & Accounts
- **Double‑entry** postings with **deferred balance check** (sum=0) and **idempotency key**.
- Account types: **Savings, Current, Fixed Deposit, Wallet**; multi‑currency ready (default NGN).
- Features: holds/liens, overdrafts, sweeps, interest accrual/capitalization, dormancy.
- Statements (PDF/CSV), real‑time balances, period locks and back‑dated corrections via reversal+new entry only.

### 3.3 Payments
- **Internal transfers** (same tenant): instant.
- **Interbank (NIP) adapter:** pluggable provider; per‑tenant credentials & network profile; name enquiry; initiate; status; reversal.
- **Scheduled/recurring** transfers; bill pay (aggregator abstraction); webhooks for inbound events.
- **Limits & approvals:** per‑role, per‑product, per‑channel; maker–checker for high‑risk thresholds.
- **Notifications:** SMS/Email real‑time debit/credit alerts with Termii/SendGrid; in‑app inbox.

### 3.4 Loans (All Types)
- Loan products: interest model (flat/reducing), tenor, fees, penalties, collateral flag, approval rules.
- Origination: customer/staff application; docs; eligibility checks (basic v1).
- Workflow: configurable **approver chain**; AI **assist** (recommend approve/decline; rationale).
- Disbursement: GL compliant entries (Loans Receivable ↔ Customer acct); fees netted if configured.
- Repayment: schedule generation; autopay; manual; advance payoff; restructure; write‑off.
- Accruals & penalties: daily cron; delinquency flags; collections queue; reminders (SMS/Email).
- Reports: portfolio, NPL, vintage, branch/product breakdown.

### 3.5 Teams, RBAC, Maker–Checker
- Roles: preset templates + custom roles; per‑permission flags (CRUD per module + actions).
- Scope: global/tenant/branch/sub‑bank scoping.
- Maker–checker: configure which actions require a checker (large transfer, write‑off, fees change).

### 3.6 Insights & AI
- Customer insights: spending categorization, budgets, nudges, saving goals.
- Bank insights: liquidity, fee/interest revenue, risk alerts, anomaly detection candidates.
- AI advisor: chat for customers & staff (“Show pending approvals”), **loan approval suggestions**.
- Toggle per tenant; model calls via AI service; strict minimization of PII.

### 3.7 Reporting & Reconciliation
- Regulatory: daily journal, trial balance, income sheets, loan aging, high‑value tx logs.
- Operational: EOD reports by branch, exception queues (failed NIP, partial posts).
- Reconciliation: external statements vs postings; auto‑match; exception workflow.
- Exports: CSV/PDF; S3-like object storage for generated files with signed URLs.

### 3.8 Notifications
- Channels: **Termii SMS**, **SendGrid Email**, in‑app; push (future).
- Templates per tenant; rate limits; delivery logs & retries; digest jobs.

---

## 4) Non‑Functional Requirements
- **Performance:** p50 < 200ms, p95 < 600ms for typical reads; 1k+ postings/sec sustained on standard SKU.
- **Availability:** 99.9% monthly; graceful degradation for non‑critical services.
- **Security:** MFA everywhere; TLS 1.3; **encryption at rest**; principle of least privilege; WORM audit.
- **Compliance:** NDPR; CBN expectations; audit evidence (access logs, approvals, change mgmt).
- **Observability:** structured logs, metrics (Prometheus), tracing (OTel); alerting (95th latency, error rate).

---

## 5) System Architecture

### 5.1 Monorepo Layout
```
/lenz-core/
  apps/
    backend/            # Go (GORM) REST API + workers
    frontend/           # React + Vite + TS (tenant-themed)
  packages/
    shared/             # shared models (OpenAPI types), validation, utils
  infra/
    docker/             # docker-compose, Dockerfiles
    migrations/         # SQL migrations (golang-migrate) + seeds
    scripts/            # make targets, CI helpers
  docs/
    PRD.md              # this file
    API.md              # generated OpenAPI + how-to
    SECURITY.md         # threat model, keys, rotations
```

### 5.2 Backend Modules (Go / GORM)
- **auth**: sessions, MFA (TOTP + SMS), device mgmt.
- **tenants**: institutions, branches, sub‑banks, branding, feature flags.
- **users & rbac**: roles, permissions, maker–checker policies.
- **kyc**: customers, docs, BVN field capture, verification hooks.
- **ledger**: journal, postings, balances, holds/liens, EOD locks.
- **accounts**: open/close, interest configs, overdraft limits.
- **payments**: internal transfers, NIP adapter, bill pay adapters, webhooks.
- **loans**: products, origination, approval, schedules, accruals, collections.
- **notifications**: Termii/SendGrid clients, templates, queueing.
- **reports**: predefined reports, GL export, statement generation.
- **insights**: analytics jobs, AI service client.
- **audit**: append‑only audit events; tamper‑evident hashing.
- **ops**: cron scheduler, reconciliation, backfills.

**Libraries:** `gorm.io/gorm`, `gorm.io/driver/postgres`, `github.com/shopspring/decimal`, `golang.org/x/crypto`, `github.com/go-chi/chi` or `gin-gonic/gin`, `github.com/golang-jwt/jwt/v5`, `github.com/robfig/cron/v3`.

### 5.3 Database (PostgreSQL)
Key tables (all include `tenant_id` unless noted):
- `institutions`, `branches`, `sub_banks`
- `users`, `roles`, `role_permissions`, `user_roles`, `mfa_devices`
- `customers`, `kyc_documents`
- `accounts` (`number`, `currency`, `status`, `product_code`, `branch_id`)
- `journal_entries` (`idempotency_key`, `booking_time`, `value_time`, `narrative`)
- `postings` (`entry_id`, `account_id`, `dc`, `amount` DECIMAL(24,6))
- `account_balances` (`current_amount`, `available_amount`, `version`)
- `holds` / `liens`
- `transfers` (internal + external NIP metadata)
- `loan_products`, `loans`, `loan_schedules`, `loan_events`
- `notifications`, `email_templates`, `sms_templates`
- `audit_events`, `webhooks`, `reports`

**Indices & constraints:**
- Composite unique: `(tenant_id, account_number)`.
- Postings defer‑constraint to enforce sum=0 per `entry_id` (trigger).
- Hot paths: indexes on `(tenant_id, customer_id)`, `(tenant_id, booking_time)`.

### 5.4 Encryption & Key Management
**Recommended:** **HashiCorp Vault Transit** or Cloud **KMS** (GCP/AWS) for envelope encryption.  
**Alternative (acceptable v1):** a **dedicated encryption microservice** with HSM‑backed keys, network‑isolated, 3‑admin quorum.  
- Secrets never stored in app env; app requests encrypt/decrypt via mTLS to KMS/Transit.
- Per‑tenant data keys (DEKs) derived from master; rotating keys supported.
- Fields encrypted at app layer: PII (BVN, NIN), secrets, API keys, tokens.

### 5.5 Security Controls
- **MFA mandatory** for login AND high‑risk actions (transfers, approvals, config changes).
- **Maker–checker** enforced server‑side; approvals immutable in audit.
- JWT access tokens (short TTL) + refresh; device binding; IP rate‑limit per route.
- Least‑privilege DB roles; RLS optional (backed by app scoping) for belt‑and‑suspenders.
- TLS 1.3 everywhere; CSP/HTTPS‑only; secure cookies; CSRF for web.
- Full **audit** (WORM): hash‑chain batches to detect tampering.

### 5.6 Jobs & Cron
- **Interest accrual** (daily); **interest capitalization** (monthly EOM).
- **Loan accruals** & penalties (daily); **delinquency escalations**.
- **Statements** (monthly); **digest notifications** (daily/weekly).
- **Scheduled transfers** executor; **reconciliation** workers.
- **Data hygiene:** OTP expiry purge, soft delete gc, log rotation.
- **Insights**: nightly feature aggregation for AI.

### 5.7 Observability
- Structured JSON logs (request id, tenant id, user id, route, latency, status).
- Prometheus metrics (QPS, errors, p95 latency, DB pool stats); Grafana dashboards.
- OpenTelemetry traces; alert rules (5xx > threshold, latency spikes, job failures).

---

## 6) Frontend (React + **Vite** + TypeScript)
### 6.1 App Shell & Theming
- Vite TS SPA; router‑based code splitting.
- Theme via CSS variables; fetched at boot: `GET /tenants/:id/theme` by subdomain.
- Assets: per‑tenant logos, email/SMS templates preview.

### 6.2 Modules (Customer Portal)
- **Dashboard** (balances, insights cards, AI chat).
- **Accounts** (list, details, statements download).
- **Transfers** (internal/NIP; beneficiaries; OTP confirm; limits UI).
- **Loans** (apply, view schedule, repay; offers from AI).
- **Notifications** (in‑app inbox).
- **Profile & Security** (PII edit with OTP; **MFA setup** TOTP/SMS).

### 6.3 Modules (Staff / Admin)
- **Ops Dashboard** (KPI by branch/sub‑bank).
- **Customers** (search, KYC, risk flags).
- **Accounts** (open/close/freeze; holds/liens; interest config).
- **Payments** (queues; NIP statuses; reversals; reconciliation).
- **Loans** (origination → approval → servicing → collections).
- **Approvals** (maker–checker worklist).
- **Teams & Roles** (users, roles, permissions, scoping).
- **Settings** (branding, fees, limits, branches, providers).
- **Reports** (view/export; schedule).

### 6.4 Security (FE)
- Auth flows: login → MFA → device remember; session timeout.
- Step‑up MFA prompts on high‑risk actions.
- CSRF protection for web forms; secure storage (httpOnly cookies for tokens).

---

## 7) API — Design, Endpoints & Postman
- **Style:** REST, JSON; `Authorization: Bearer <JWT>`; `X-Tenant-ID` or subdomain derived.
- **Versioning:** `/api/v1/...`
- **Idempotency:** `Idempotency-Key` header for money‑moving POSTs.
- **Errors:** JSON `{ code, message, details }`.

### 7.1 Core Endpoint Inventory (High‑level)
(Complete sample provided in Postman JSON delivered with this PRD.)

**Auth & MFA**
- `POST /api/v1/auth/login` — username/password → 1st‑factor
- `POST /api/v1/auth/mfa/verify` — OTP/TOTP verify → tokens
- `POST /api/v1/auth/logout`
- `POST /api/v1/auth/refresh`

**Tenants & Branding**
- `GET /api/v1/tenants/me`
- `PUT /api/v1/tenants/me/theme`
- `GET /api/v1/branches` / `POST /api/v1/branches`
- `GET /api/v1/subbanks` / `POST /api/v1/subbanks`

**Users & RBAC**
- `GET /api/v1/users` / `POST /api/v1/users`
- `POST /api/v1/users/:id/reset-password`
- `GET /api/v1/roles` / `POST /api/v1/roles`
- `PUT /api/v1/roles/:id/permissions`
- `POST /api/v1/mfa/setup` / `POST /api/v1/mfa/verify`

**Customers & KYC**
- `GET /api/v1/customers` / `POST /api/v1/customers`
- `GET /api/v1/customers/:id`
- `POST /api/v1/customers/:id/documents`
- `PUT /api/v1/customers/:id`

**Accounts & Ledger**
- `POST /api/v1/accounts` (open) / `GET /api/v1/accounts/:id`
- `GET /api/v1/accounts/:id/transactions`
- `POST /api/v1/accounts/:id/holds` / `DELETE .../holds/:holdId`
- `POST /api/v1/ledger/post` (internal postings; admin only)

**Transfers & Payments**
- `POST /api/v1/transfers/internal`
- `POST /api/v1/transfers/nip/name-enquiry`
- `POST /api/v1/transfers/nip/initiate`
- `GET /api/v1/transfers/:id/status`
- `POST /api/v1/transfers/:id/reverse` (maker–checker)
- `POST /api/v1/transfers/scheduled` (create) / `GET /api/v1/transfers/scheduled`

**Loans**
- `GET /api/v1/loan-products` / `POST /api/v1/loan-products`
- `POST /api/v1/loans/apply`
- `GET /api/v1/loans/:id`
- `POST /api/v1/loans/:id/approve` (maker–checker)
- `POST /api/v1/loans/:id/disburse`
- `POST /api/v1/loans/:id/repay`
- `POST /api/v1/loans/:id/restructure` / `POST /api/v1/loans/:id/writeoff`

**Notifications**
- `POST /api/v1/notifications/test/sms`
- `POST /api/v1/notifications/test/email`
- `GET /api/v1/notifications` (in‑app)

**Reports & Statements**
- `GET /api/v1/reports/trial-balance?from&to`
- `GET /api/v1/reports/loan-aging?asOf`
- `POST /api/v1/statements/:accountId/generate?from&to`

**Audit & Webhooks**
- `GET /api/v1/audit?actor&from&to`
- `POST /api/v1/webhooks/ingest` (NIP/biller inbound)

### 7.2 Postman & OpenAPI
- **OpenAPI** spec generated under `/docs/openapi.yaml` (CI keeps up‑to‑date).
- **Postman collection** (JSON) provided in `/docs/postman/lenz-core.postman_collection.json` (starter included with this PRD).

---

## 8) Data Model (Key Tables – Sketch)
```sql
-- Institutions (Tenants)
CREATE TABLE institutions (
  id UUID PRIMARY KEY,
  code TEXT UNIQUE NOT NULL,
  name TEXT NOT NULL,
  theme JSONB NOT NULL DEFAULT '{}',
  features JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Users (Staff)
CREATE TABLE users (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES institutions(id),
  email TEXT NOT NULL,
  phone TEXT,
  password_hash TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('ACTIVE','DISABLED')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, email)
);

-- Customers
CREATE TABLE customers (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES institutions(id),
  bvn TEXT,
  first_name TEXT, last_name TEXT,
  dob DATE, email TEXT, phone TEXT,
  status TEXT NOT NULL CHECK (status IN ('PENDING_KYC','ACTIVE','FROZEN','CLOSED')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Accounts
CREATE TABLE accounts (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES institutions(id),
  customer_id UUID NOT NULL REFERENCES customers(id),
  number TEXT NOT NULL,                  -- NUBAN
  currency CHAR(3) NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('ACTIVE','FROZEN','CLOSED')),
  product_code TEXT NOT NULL,
  overdraft_limit NUMERIC(24,6) NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, number)
);

-- Ledger
CREATE TABLE journal_entries (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  idempotency_key TEXT NOT NULL,
  booking_time TIMESTAMPTZ NOT NULL DEFAULT now(),
  value_time TIMESTAMPTZ NOT NULL DEFAULT now(),
  narrative TEXT,
  UNIQUE (tenant_id, idempotency_key)
);

CREATE TABLE postings (
  id BIGSERIAL PRIMARY KEY,
  tenant_id UUID NOT NULL,
  entry_id UUID NOT NULL REFERENCES journal_entries(id) ON DELETE RESTRICT,
  account_id UUID NOT NULL REFERENCES accounts(id)      ON DELETE RESTRICT,
  dc CHAR(1) NOT NULL CHECK (dc IN ('D','C')),
  amount NUMERIC(24,6) NOT NULL CHECK (amount > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE account_balances (
  account_id UUID PRIMARY KEY REFERENCES accounts(id),
  tenant_id UUID NOT NULL,
  current_amount NUMERIC(24,6) NOT NULL DEFAULT 0,
  available_amount NUMERIC(24,6) NOT NULL DEFAULT 0,
  last_entry_id UUID,
  version BIGINT NOT NULL DEFAULT 0
);
```

---

## 9) Backend Implementation Notes (GORM)
- Use **GORM** models with explicit `BeforeCreate/AfterCreate` hooks only for non‑critical logic; **avoid heavy hooks** on hot paths.
- Transactions: wrap posting flows in `db.Transaction(func(tx *gorm.DB) error { ... })`.
- Idempotency: table `journal_entries` keyed by `(tenant_id, idempotency_key)`.
- Concurrency: advisory locks per `account_id` to avoid deadlocks on double‑spends.
- Migrations: **golang-migrate** with SQL files, no AutoMigrate in prod.
- Validation: request DTOs validated with `go-playground/validator`.
- OpenAPI: generate via `swaggo` or `oapi-codegen`; CI enforces spec freshness.

**Example (posting skeleton):**
```go
func (s *LedgerSvc) Post(ctx context.Context, tenant uuid.UUID, idem string, lines []Line) (uuid.UUID, error) {
  return withTx(ctx, s.db, func(tx *gorm.DB) error {
    // Reuse entry if idempotent
    var je JournalEntry
    err := tx.Where("tenant_id = ? AND idempotency_key = ?", tenant, idem).First(&je).Error
    if err == nil { return nil } // already posted
    if !errors.Is(err, gorm.ErrRecordNotFound) { return err }

    je = JournalEntry{ID: uuid.New(), TenantID: tenant, IdempotencyKey: idem}
    if err := tx.Create(&je).Error; err != nil { return err }

    // Insert postings
    for _, ln := range lines {
      if err := tx.Create(&Posting{TenantID: tenant, EntryID: je.ID, AccountID: ln.AccountID, DC: ln.DC, Amount: ln.Amount}).Error; err != nil {
        return err
      }
    }
    // Update balances atomically (use SELECT FOR UPDATE or version)
    // ...

    return nil
  })
}
```

---

## 10) Security & Encryption Design
- **MFA everywhere:** default TOTP + SMS fallback; enforce step‑up on: large transfer, config change, user management, loan approval/disbursement.
- **Encryption:** prefer **Vault Transit/KMS**. App passes plaintext → gets ciphertext; decrypt only when needed. Rotate data keys quarterly; keep key IDs with ciphertext.
- **Secrets:** store Termii/SendGrid/Provider secrets in Vault/KMS; never in repo; envs get short‑lived tokens.
- **Audit:** append‑only table; nightly hash‑chain; export to cold storage; reports for regulators.
- **Threat model:** credential stuffing (rate limit + MFA), session hijack (httpOnly, SameSite), SQLi (param queries), XSS (CSP, encoding), SSRF (egress allow‑list), privilege escalation (RBAC tests).

---

## 11) DevEx, Tooling & CI/CD
- **Docker Compose**: Postgres, Redis, MailHog (local), Mock Termii.
- **Makefile**: `make dev`, `make migrate`, `make seed`, `make test`, `make lint`, `make openapi`.
- **CI**: lint, tests, migrations dry‑run, OpenAPI diff; build artifacts.
- **CD**: blue/green deploy; DB migrations gated; feature flags per tenant.

---

## 12) Acceptance Criteria (MVP)
- Create tenant → brand it → invite staff with MFA → open customer → create account (NUBAN) → internal transfer with alerts → schedule interest accrual → create loan product → submit + approve loan → disburse → repay (auto + manual) → generate statements → run trial balance → see audit trail.
- All high‑risk actions require MFA + (where configured) maker–checker.
- Postman tests pass; seed script provisions demo tenant and flows.

---

## 13) README Blueprint (to place in `/README.md`)
- Quick start (Docker Compose), env vars, running backend/frontend, migrations, seeding.
- How multi‑tenancy works; tenant scoping; branding via subdomain.
- Security setup (Vault/KMS, MFA providers, Termii & SendGrid config).
- How to run jobs, generate statements, reconcile.
- How to import **Postman collection** and explore endpoints.
- Troubleshooting & playbooks (failed NIP, stuck postings, approvals).

---

## 14) Postman Collection (Starter)
A starter collection is provided with folders for Auth, Tenants, Users, Customers, Accounts, Transfers, Loans, Notifications, Reports, Audit. Use `{{base_url}}`, `{{token}}`, `{{tenant_id}}` env vars.

> File: `/docs/postman/lenz-core.postman_collection.json`

---

## 15) Roadmap (Post‑MVP)
- Credit scoring integration; eNaira adapter; USSD channel; open banking APIs; card issuing (via partner CMS); per‑tenant data‑keying; RLS hardened; mobile apps; real‑time anomaly detection.

---

## 16) Glossary
- **NUBAN**: Nigerian Uniform Bank Account Number (10‑digit).  
- **NIP**: NIBSS Instant Payment.  
- **RLS**: Row‑Level Security.  
- **WORM**: Write Once Read Many (immutable logging).

---

© 2025 Lenz / Clive Alliance. All rights reserved.
