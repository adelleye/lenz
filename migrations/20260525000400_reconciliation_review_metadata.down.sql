DROP INDEX IF EXISTS transfers_reconciliation_queue_idx;

ALTER TABLE transfers
    DROP CONSTRAINT IF EXISTS transfers_review_status_check,
    DROP COLUMN IF EXISTS reviewed_by,
    DROP COLUMN IF EXISTS reviewed_at,
    DROP COLUMN IF EXISTS review_note,
    DROP COLUMN IF EXISTS review_status;
