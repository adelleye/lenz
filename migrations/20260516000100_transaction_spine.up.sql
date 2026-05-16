CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS currencies (
    id varchar(3) PRIMARY KEY,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS countries (
    id varchar(2) PRIMARY KEY,
    name text NOT NULL,
    flag text,
    currency varchar(3) NOT NULL,
    is_supported boolean NOT NULL DEFAULT false,
    meta jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS institutions (
    id uuid PRIMARY KEY,
    name varchar(200) NOT NULL,
    short_name varchar(50) NOT NULL,
    code varchar(10) NOT NULL,
    nuban_prefix varchar(3) NOT NULL,
    country_id varchar(2) NOT NULL REFERENCES countries(id),
    currency_id varchar(3) NOT NULL REFERENCES currencies(id),
    status varchar(20) NOT NULL DEFAULT 'pending',
    meta jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE UNIQUE INDEX IF NOT EXISTS institutions_code_uq ON institutions(code);

CREATE TABLE IF NOT EXISTS branches (
    id uuid PRIMARY KEY,
    institution_id uuid NOT NULL REFERENCES institutions(id) ON DELETE CASCADE,
    code varchar(10) NOT NULL,
    name text NOT NULL,
    meta jsonb NOT NULL DEFAULT '{}'::jsonb,
    status varchar(20) NOT NULL DEFAULT 'pending',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (institution_id, code)
);

CREATE TABLE IF NOT EXISTS customers (
    id uuid PRIMARY KEY,
    institution_id uuid NOT NULL REFERENCES institutions(id) ON DELETE CASCADE,
    branch_id uuid NOT NULL REFERENCES branches(id),
    first_name varchar(200) NOT NULL,
    last_name varchar(200) NOT NULL,
    middle_name varchar(200),
    email varchar(200) NOT NULL,
    phone varchar(30) NOT NULL,
    status varchar(20) NOT NULL DEFAULT 'pending',
    meta jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS customers_institution_idx ON customers(institution_id);

CREATE TABLE IF NOT EXISTS accounts (
    id uuid PRIMARY KEY,
    institution_id uuid NOT NULL REFERENCES institutions(id) ON DELETE CASCADE,
    customer_id uuid REFERENCES customers(id),
    account_number varchar(20) NOT NULL,
    name text NOT NULL,
    kind varchar(20) NOT NULL CHECK (kind IN ('customer', 'internal')),
    currency_id varchar(3) NOT NULL REFERENCES currencies(id),
    normal_balance varchar(10) NOT NULL CHECK (normal_balance IN ('debit', 'credit')),
    status varchar(20) NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (institution_id, account_number)
);

CREATE INDEX IF NOT EXISTS accounts_customer_idx ON accounts(institution_id, customer_id);

CREATE TABLE IF NOT EXISTS account_balances (
    account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    institution_id uuid NOT NULL REFERENCES institutions(id) ON DELETE CASCADE,
    available_minor bigint NOT NULL DEFAULT 0,
    ledger_minor bigint NOT NULL DEFAULT 0,
    currency_id varchar(3) NOT NULL REFERENCES currencies(id),
    last_journal_entry_id uuid,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (available_minor = ledger_minor)
);

CREATE INDEX IF NOT EXISTS account_balances_institution_idx ON account_balances(institution_id, account_id);

CREATE TABLE IF NOT EXISTS journal_entries (
    id uuid PRIMARY KEY,
    institution_id uuid NOT NULL REFERENCES institutions(id) ON DELETE CASCADE,
    transfer_id uuid,
    entry_type varchar(20) NOT NULL CHECK (entry_type IN ('inbound', 'outbound', 'reversal')),
    currency_id varchar(3) NOT NULL REFERENCES currencies(id),
    narration text NOT NULL,
    total_debit_minor bigint NOT NULL CHECK (total_debit_minor >= 0),
    total_credit_minor bigint NOT NULL CHECK (total_credit_minor >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (total_debit_minor = total_credit_minor)
);

CREATE INDEX IF NOT EXISTS journal_entries_institution_idx ON journal_entries(institution_id, created_at DESC);

CREATE TABLE IF NOT EXISTS postings (
    id uuid PRIMARY KEY,
    institution_id uuid NOT NULL REFERENCES institutions(id) ON DELETE CASCADE,
    journal_entry_id uuid NOT NULL REFERENCES journal_entries(id) ON DELETE CASCADE,
    account_id uuid NOT NULL REFERENCES accounts(id),
    direction varchar(10) NOT NULL CHECK (direction IN ('debit', 'credit')),
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency_id varchar(3) NOT NULL REFERENCES currencies(id),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS postings_account_idx ON postings(institution_id, account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS postings_journal_idx ON postings(institution_id, journal_entry_id);

CREATE TABLE IF NOT EXISTS transfers (
    id uuid PRIMARY KEY,
    institution_id uuid NOT NULL REFERENCES institutions(id) ON DELETE CASCADE,
    account_id uuid NOT NULL REFERENCES accounts(id),
    direction varchar(20) NOT NULL CHECK (direction IN ('inbound', 'outbound', 'reversal')),
    status varchar(20) NOT NULL CHECK (status IN ('pending', 'succeeded', 'failed')),
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency_id varchar(3) NOT NULL REFERENCES currencies(id),
    idempotency_key text NOT NULL,
    provider text NOT NULL,
    provider_reference text NOT NULL DEFAULT '',
    provider_event_id text,
    journal_entry_id uuid REFERENCES journal_entries(id),
    reversal_of_transfer_id uuid REFERENCES transfers(id),
    failure_reason text,
    narration text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (institution_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS transfers_account_idx ON transfers(institution_id, account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS transfers_status_idx ON transfers(institution_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS provider_events (
    id uuid PRIMARY KEY,
    institution_id uuid NOT NULL REFERENCES institutions(id) ON DELETE CASCADE,
    provider text NOT NULL,
    provider_event_id text NOT NULL,
    provider_reference text NOT NULL DEFAULT '',
    transfer_id uuid REFERENCES transfers(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (institution_id, provider, provider_event_id)
);

CREATE TABLE IF NOT EXISTS audit_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    institution_id uuid NOT NULL REFERENCES institutions(id) ON DELETE CASCADE,
    actor text NOT NULL DEFAULT 'system',
    action text NOT NULL,
    subject_type text NOT NULL,
    subject_id text NOT NULL,
    meta jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
