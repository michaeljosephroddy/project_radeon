ALTER TABLE users
ADD COLUMN IF NOT EXISTS is_available_to_support BOOLEAN NOT NULL DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS support_modes TEXT[] NOT NULL DEFAULT '{}',
ADD COLUMN IF NOT EXISTS support_updated_at TIMESTAMP NULL;

CREATE TABLE IF NOT EXISTS support_requests (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    requester_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN (
        'need_to_talk',
        'need_distraction',
        'need_encouragement',
        'need_company'
    )),
    message TEXT,
    audience TEXT NOT NULL CHECK (audience IN (
        'followers',
        'city',
        'community'
    )),
    city TEXT,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN (
        'open',
        'matched',
        'closed',
        'expired'
    )),
    matched_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    closed_at TIMESTAMP NULL
);

CREATE TABLE IF NOT EXISTS support_responses (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    support_request_id UUID NOT NULL REFERENCES support_requests(id) ON DELETE CASCADE,
    responder_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    response_type TEXT NOT NULL CHECK (response_type IN (
        'can_chat',
        'check_in_later',
        'nearby'
    )),
    message TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (support_request_id, responder_id, response_type)
);

CREATE INDEX IF NOT EXISTS idx_support_requests_requester_created_at
ON support_requests(requester_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_requests_status_expires_at
ON support_requests(status, expires_at);

CREATE INDEX IF NOT EXISTS idx_support_requests_city_status_expires_at
ON support_requests(city, status, expires_at);

CREATE INDEX IF NOT EXISTS idx_support_responses_request_created_at
ON support_responses(support_request_id, created_at);
