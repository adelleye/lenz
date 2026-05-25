DROP INDEX IF EXISTS audit_events_account_idx;
DROP INDEX IF EXISTS audit_events_transfer_idx;
DROP INDEX IF EXISTS audit_events_institution_created_idx;

ALTER TABLE audit_events
    DROP COLUMN IF EXISTS metadata,
    DROP COLUMN IF EXISTS new_status,
    DROP COLUMN IF EXISTS old_status,
    DROP COLUMN IF EXISTS reference,
    DROP COLUMN IF EXISTS idempotency_key,
    DROP COLUMN IF EXISTS journal_entry_id,
    DROP COLUMN IF EXISTS transfer_id,
    DROP COLUMN IF EXISTS account_id,
    DROP COLUMN IF EXISTS customer_id,
    DROP COLUMN IF EXISTS entity_id,
    DROP COLUMN IF EXISTS entity_type,
    DROP COLUMN IF EXISTS request_id,
    DROP COLUMN IF EXISTS actor_id,
    DROP COLUMN IF EXISTS actor_type;
