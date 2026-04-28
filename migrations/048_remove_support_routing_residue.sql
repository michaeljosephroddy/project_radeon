DROP INDEX IF EXISTS idx_support_requests_routing_status_created_at;
DROP INDEX IF EXISTS idx_support_requests_priority_expires_at;
DROP INDEX IF EXISTS idx_support_offers_responder_status_expires_at;
DROP INDEX IF EXISTS idx_support_offers_request_status;
DROP INDEX IF EXISTS idx_support_events_request_created_at;

DROP TABLE IF EXISTS support_events;
DROP TABLE IF EXISTS support_offers;
DROP TABLE IF EXISTS support_responder_stats;
DROP TABLE IF EXISTS support_responder_presence;
DROP TABLE IF EXISTS support_responder_profiles;

ALTER TABLE support_requests
    DROP CONSTRAINT IF EXISTS support_requests_routing_status_check,
    DROP CONSTRAINT IF EXISTS support_requests_desired_response_window_check;

ALTER TABLE support_requests
    DROP COLUMN IF EXISTS routing_status,
    DROP COLUMN IF EXISTS desired_response_window,
    DROP COLUMN IF EXISTS priority_visibility,
    DROP COLUMN IF EXISTS priority_expires_at;
