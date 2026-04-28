DROP INDEX IF EXISTS idx_support_requests_status_expires_at;
DROP INDEX IF EXISTS idx_support_requests_city_status_expires_at;
DROP INDEX IF EXISTS idx_support_requests_requester_channel_created_at;

UPDATE support_requests
SET
    status = 'closed',
    closed_at = COALESCE(closed_at, NOW())
WHERE status = 'expired';

ALTER TABLE support_requests
    DROP CONSTRAINT IF EXISTS support_requests_status_check;

ALTER TABLE support_requests
    ADD CONSTRAINT support_requests_status_check
        CHECK (status IN ('open', 'active', 'closed'));

ALTER TABLE support_requests
    DROP CONSTRAINT IF EXISTS support_requests_matched_user_id_fkey;

ALTER TABLE support_requests
    DROP COLUMN IF EXISTS matched_user_id,
    DROP COLUMN IF EXISTS expires_at;
