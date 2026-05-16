DROP TABLE IF EXISTS account_holds;

DROP INDEX IF EXISTS transfers_pending_provider_reference_idx;
DROP INDEX IF EXISTS transfers_provider_reference_idx;
DROP INDEX IF EXISTS transfers_institution_created_idx;

ALTER TABLE transfers
    DROP COLUMN IF EXISTS reconciliation_status,
    DROP COLUMN IF EXISTS ledger_status,
    DROP COLUMN IF EXISTS provider_status;

ALTER TABLE account_balances
    ADD CONSTRAINT account_balances_check CHECK (available_minor = ledger_minor);

ALTER TABLE accounts
    DROP COLUMN IF EXISTS allow_negative_balance,
    DROP COLUMN IF EXISTS product_type;
