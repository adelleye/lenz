ALTER TABLE audit_events
    ADD COLUMN IF NOT EXISTS actor_type text NOT NULL DEFAULT 'system',
    ADD COLUMN IF NOT EXISTS actor_id text NOT NULL DEFAULT 'system',
    ADD COLUMN IF NOT EXISTS request_id text NOT NULL DEFAULT 'service',
    ADD COLUMN IF NOT EXISTS entity_type text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS entity_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS customer_id uuid,
    ADD COLUMN IF NOT EXISTS account_id uuid,
    ADD COLUMN IF NOT EXISTS transfer_id uuid,
    ADD COLUMN IF NOT EXISTS journal_entry_id uuid,
    ADD COLUMN IF NOT EXISTS idempotency_key text,
    ADD COLUMN IF NOT EXISTS reference text,
    ADD COLUMN IF NOT EXISTS old_status text,
    ADD COLUMN IF NOT EXISTS new_status text,
    ADD COLUMN IF NOT EXISTS metadata jsonb NOT NULL DEFAULT '{}'::jsonb;

UPDATE audit_events
SET entity_type = subject_type,
    entity_id = subject_id,
    metadata = meta
WHERE entity_type = '';

CREATE INDEX IF NOT EXISTS audit_events_institution_created_idx
    ON audit_events(institution_id, created_at DESC);

CREATE INDEX IF NOT EXISTS audit_events_transfer_idx
    ON audit_events(institution_id, transfer_id)
    WHERE transfer_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS audit_events_account_idx
    ON audit_events(institution_id, account_id, created_at DESC)
    WHERE account_id IS NOT NULL;
