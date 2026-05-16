ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS product_type varchar(40) NOT NULL DEFAULT 'standard_wallet',
    ADD COLUMN IF NOT EXISTS allow_negative_balance boolean NOT NULL DEFAULT false;

UPDATE accounts
SET product_type = 'internal',
    allow_negative_balance = true
WHERE kind = 'internal';

ALTER TABLE account_balances DROP CONSTRAINT IF EXISTS account_balances_check;

ALTER TABLE transfers
    ADD COLUMN IF NOT EXISTS provider_status varchar(20) NOT NULL DEFAULT 'pending' CHECK (provider_status IN ('pending', 'succeeded', 'failed')),
    ADD COLUMN IF NOT EXISTS ledger_status varchar(30) NOT NULL DEFAULT 'pending' CHECK (ledger_status IN ('pending', 'posted', 'no_posting', 'reversal_deficit')),
    ADD COLUMN IF NOT EXISTS reconciliation_status varchar(30) NOT NULL DEFAULT 'pending' CHECK (reconciliation_status IN ('pending', 'matched', 'no_action', 'manual_review'));

UPDATE transfers
SET provider_status = status,
    ledger_status = CASE
        WHEN status = 'succeeded' THEN 'posted'
        WHEN status = 'failed' THEN 'no_posting'
        ELSE 'pending'
    END,
    reconciliation_status = CASE
        WHEN status = 'succeeded' THEN 'matched'
        WHEN status = 'failed' THEN 'no_action'
        ELSE 'pending'
    END;

CREATE INDEX IF NOT EXISTS transfers_institution_created_idx ON transfers(institution_id, created_at DESC);
CREATE INDEX IF NOT EXISTS transfers_provider_reference_idx ON transfers(institution_id, provider, provider_reference);
CREATE INDEX IF NOT EXISTS transfers_pending_provider_reference_idx
    ON transfers(institution_id, provider, provider_reference, direction, created_at)
    WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS account_holds (
    id uuid PRIMARY KEY,
    institution_id uuid NOT NULL REFERENCES institutions(id) ON DELETE CASCADE,
    account_id uuid NOT NULL REFERENCES accounts(id),
    transfer_id uuid NOT NULL REFERENCES transfers(id),
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency_id varchar(3) NOT NULL REFERENCES currencies(id),
    status varchar(20) NOT NULL CHECK (status IN ('active', 'released', 'consumed')),
    reason text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    released_at timestamptz,
    UNIQUE (institution_id, transfer_id)
);

CREATE INDEX IF NOT EXISTS account_holds_account_status_idx ON account_holds(institution_id, account_id, status);
