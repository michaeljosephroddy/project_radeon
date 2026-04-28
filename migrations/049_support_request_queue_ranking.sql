ALTER TABLE support_requests
    ADD COLUMN IF NOT EXISTS last_response_at TIMESTAMPTZ;

UPDATE support_requests sr
SET last_response_at = latest.last_response_at
FROM (
    SELECT support_request_id, MAX(created_at) AS last_response_at
    FROM support_responses
    GROUP BY support_request_id
) latest
WHERE sr.id = latest.support_request_id;

CREATE INDEX IF NOT EXISTS idx_support_requests_open_queue
    ON support_requests(channel, created_at, id)
    WHERE status = 'open';

CREATE INDEX IF NOT EXISTS idx_support_responses_responder_request
    ON support_responses(responder_id, support_request_id);
