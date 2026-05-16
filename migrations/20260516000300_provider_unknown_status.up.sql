ALTER TABLE transfers
    DROP CONSTRAINT IF EXISTS transfers_provider_status_check,
    ADD CONSTRAINT transfers_provider_status_check
        CHECK (provider_status IN ('pending', 'succeeded', 'failed', 'provider_unknown'));
