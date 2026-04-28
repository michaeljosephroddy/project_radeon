ALTER TABLE support_requests
    ADD COLUMN IF NOT EXISTS accepted_response_id UUID NULL,
    ADD COLUMN IF NOT EXISTS accepted_responder_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS accepted_at TIMESTAMP NULL,
    ADD COLUMN IF NOT EXISTS chat_id UUID NULL REFERENCES chats(id) ON DELETE SET NULL;

ALTER TABLE support_requests
    DROP CONSTRAINT IF EXISTS support_requests_status_check;

UPDATE support_requests
SET status = 'active'
WHERE status = 'matched';

ALTER TABLE support_requests
    ADD CONSTRAINT support_requests_status_check
        CHECK (status IN ('open', 'active', 'closed', 'expired'));

UPDATE support_requests sr
SET
    accepted_responder_id = COALESCE(
        sr.accepted_responder_id,
        (
            SELECT so.responder_id
            FROM support_offers so
            WHERE so.support_request_id = sr.id
              AND so.status IN ('accepted', 'closed')
            ORDER BY COALESCE(so.responded_at, so.closed_at, so.offered_at) DESC
            LIMIT 1
        )
    ),
    accepted_at = COALESCE(
        sr.accepted_at,
        (
            SELECT COALESCE(so.responded_at, so.closed_at, so.offered_at)
            FROM support_offers so
            WHERE so.support_request_id = sr.id
              AND so.status IN ('accepted', 'closed')
            ORDER BY COALESCE(so.responded_at, so.closed_at, so.offered_at) DESC
            LIMIT 1
        )
    ),
    chat_id = COALESCE(
        sr.chat_id,
        (
            SELECT ch.id
            FROM chats ch
            WHERE ch.support_request_id = sr.id
              AND ch.is_group = false
            ORDER BY ch.created_at DESC
            LIMIT 1
        )
    )
WHERE sr.status IN ('active', 'closed')
  AND (
      sr.accepted_responder_id IS NULL
      OR sr.accepted_at IS NULL
      OR sr.chat_id IS NULL
  );

ALTER TABLE support_responses
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'pending';

ALTER TABLE support_responses
    DROP CONSTRAINT IF EXISTS support_responses_status_check;

ALTER TABLE support_responses
    ADD CONSTRAINT support_responses_status_check
        CHECK (status IN ('pending', 'accepted', 'not_selected'));

UPDATE support_requests sr
SET accepted_response_id = (
    SELECT rsp.id
    FROM support_responses rsp
    WHERE rsp.support_request_id = sr.id
      AND (sr.accepted_responder_id IS NULL OR rsp.responder_id = sr.accepted_responder_id)
    ORDER BY
        CASE WHEN sr.chat_id IS NOT NULL AND rsp.chat_id = sr.chat_id THEN 0 ELSE 1 END,
        rsp.created_at DESC
    LIMIT 1
)
WHERE sr.status IN ('active', 'closed')
  AND sr.accepted_response_id IS NULL;

UPDATE support_responses rsp
SET status = CASE
    WHEN sr.accepted_response_id = rsp.id THEN 'accepted'
    WHEN sr.accepted_response_id IS NOT NULL THEN 'not_selected'
    ELSE 'pending'
END
FROM support_requests sr
WHERE sr.id = rsp.support_request_id;

UPDATE support_requests
SET routing_status = 'not_applicable'
WHERE routing_status <> 'not_applicable';

UPDATE support_offers
SET
    status = 'closed',
    closed_at = COALESCE(closed_at, NOW())
WHERE status IN ('pending', 'accepted');

ALTER TABLE support_requests
    DROP CONSTRAINT IF EXISTS support_requests_accepted_response_id_fkey;

ALTER TABLE support_requests
    ADD CONSTRAINT support_requests_accepted_response_id_fkey
        FOREIGN KEY (accepted_response_id) REFERENCES support_responses(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_support_requests_accepted_responder_status_created_at
    ON support_requests(accepted_responder_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_requests_requester_status_created_at
    ON support_requests(requester_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_responses_request_status_created_at
    ON support_responses(support_request_id, status, created_at DESC);
