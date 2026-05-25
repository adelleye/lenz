ALTER TABLE account_holds
    DROP CONSTRAINT IF EXISTS account_holds_institution_id_transfer_id_key;

ALTER TABLE account_holds
    ALTER COLUMN transfer_id DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS reference text NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS account_holds_transfer_unique_idx
    ON account_holds(institution_id, transfer_id)
    WHERE transfer_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS account_holds_active_lien_reference_idx
    ON account_holds(institution_id, account_id, reference)
    WHERE transfer_id IS NULL AND status = 'active' AND reference <> '';

CREATE INDEX IF NOT EXISTS account_holds_active_lien_idx
    ON account_holds(institution_id, account_id, status)
    WHERE transfer_id IS NULL;
