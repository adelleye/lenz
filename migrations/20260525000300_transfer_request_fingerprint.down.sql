ALTER TABLE provider_events
DROP COLUMN IF EXISTS request_fingerprint;

ALTER TABLE transfers
DROP COLUMN IF EXISTS request_fingerprint;
