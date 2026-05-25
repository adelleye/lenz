ALTER TABLE transfers
ADD COLUMN IF NOT EXISTS request_fingerprint text NOT NULL DEFAULT '';

ALTER TABLE provider_events
ADD COLUMN IF NOT EXISTS request_fingerprint text NOT NULL DEFAULT '';
