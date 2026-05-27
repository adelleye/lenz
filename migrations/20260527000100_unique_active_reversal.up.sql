CREATE UNIQUE INDEX IF NOT EXISTS transfers_one_active_reversal_idx
    ON transfers(institution_id, reversal_of_transfer_id)
    WHERE direction = 'reversal' AND status IN ('succeeded', 'pending');
