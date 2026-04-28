ALTER TABLE support_requests
    DROP CONSTRAINT IF EXISTS support_requests_matched_session_id_fkey;

DROP INDEX IF EXISTS idx_support_sessions_requester_status_created_at;
DROP INDEX IF EXISTS idx_support_sessions_responder_status_created_at;
DROP INDEX IF EXISTS idx_support_events_session_created_at;

ALTER TABLE support_events
    DROP CONSTRAINT IF EXISTS support_events_support_session_id_fkey;

ALTER TABLE support_requests
    DROP COLUMN IF EXISTS matched_session_id;

ALTER TABLE support_events
    DROP COLUMN IF EXISTS support_session_id;

DROP TABLE IF EXISTS support_sessions;
