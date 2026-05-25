DROP INDEX IF EXISTS account_holds_active_lien_idx;
DROP INDEX IF EXISTS account_holds_active_lien_reference_idx;
DROP INDEX IF EXISTS account_holds_transfer_unique_idx;

DELETE FROM account_holds WHERE transfer_id IS NULL;

ALTER TABLE account_holds
    ALTER COLUMN transfer_id SET NOT NULL,
    DROP COLUMN IF EXISTS reference;

ALTER TABLE account_holds
    ADD CONSTRAINT account_holds_institution_id_transfer_id_key UNIQUE (institution_id, transfer_id);
