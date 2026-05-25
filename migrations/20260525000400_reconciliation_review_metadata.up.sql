ALTER TABLE transfers
    ADD COLUMN IF NOT EXISTS review_status varchar(40),
    ADD COLUMN IF NOT EXISTS review_note text,
    ADD COLUMN IF NOT EXISTS reviewed_at timestamptz,
    ADD COLUMN IF NOT EXISTS reviewed_by text;

ALTER TABLE transfers
    DROP CONSTRAINT IF EXISTS transfers_review_status_check,
    ADD CONSTRAINT transfers_review_status_check
        CHECK (review_status IS NULL OR review_status IN ('reviewed', 'resolved_no_action', 'manual_followup_required'));

CREATE INDEX IF NOT EXISTS transfers_reconciliation_queue_idx
    ON transfers(institution_id, created_at DESC, id DESC)
    WHERE review_status IS NULL OR review_status = 'manual_followup_required';
