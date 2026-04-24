-- Add urgency to support requests
ALTER TABLE support_requests
    ADD COLUMN IF NOT EXISTS urgency text NOT NULL DEFAULT 'when_you_can';

-- Make expires_at nullable (requests no longer auto-expire)
ALTER TABLE support_requests
    ALTER COLUMN expires_at DROP NOT NULL;

-- Clear expires_at on all currently open requests so they remain visible
UPDATE support_requests SET expires_at = NULL WHERE status = 'open';

-- Drop audience column (all requests are now community-wide)
ALTER TABLE support_requests
    DROP COLUMN IF EXISTS audience;

-- Convert support_modes array to single support_mode string
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS support_mode text;

UPDATE users
    SET support_mode = support_modes[1]
    WHERE support_modes IS NOT NULL AND array_length(support_modes, 1) > 0;

ALTER TABLE users
    DROP COLUMN IF EXISTS support_modes;
