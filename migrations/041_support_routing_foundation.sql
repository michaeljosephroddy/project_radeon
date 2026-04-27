ALTER TABLE support_requests
    ADD COLUMN IF NOT EXISTS channel TEXT NOT NULL DEFAULT 'community',
    ADD COLUMN IF NOT EXISTS routing_status TEXT NOT NULL DEFAULT 'not_applicable',
    ADD COLUMN IF NOT EXISTS desired_response_window TEXT NOT NULL DEFAULT 'when_you_can',
    ADD COLUMN IF NOT EXISTS privacy_level TEXT NOT NULL DEFAULT 'standard',
    ADD COLUMN IF NOT EXISTS matched_session_id UUID NULL;

ALTER TABLE support_requests
    DROP CONSTRAINT IF EXISTS support_requests_channel_check,
    DROP CONSTRAINT IF EXISTS support_requests_routing_status_check,
    DROP CONSTRAINT IF EXISTS support_requests_desired_response_window_check,
    DROP CONSTRAINT IF EXISTS support_requests_privacy_level_check;

ALTER TABLE support_requests
    ADD CONSTRAINT support_requests_channel_check
        CHECK (channel IN ('immediate', 'community')),
    ADD CONSTRAINT support_requests_routing_status_check
        CHECK (routing_status IN ('pending', 'offered', 'matched', 'fallback', 'closed', 'not_applicable')),
    ADD CONSTRAINT support_requests_desired_response_window_check
        CHECK (desired_response_window IN ('right_now', 'soon', 'when_you_can')),
    ADD CONSTRAINT support_requests_privacy_level_check
        CHECK (privacy_level IN ('standard', 'private'));

UPDATE support_requests
SET
    channel = 'community',
    routing_status = 'not_applicable',
    desired_response_window = CASE urgency
        WHEN 'right_now' THEN 'right_now'
        WHEN 'soon' THEN 'soon'
        ELSE 'when_you_can'
    END,
    privacy_level = 'standard'
WHERE channel IS NULL
   OR routing_status IS NULL
   OR desired_response_window IS NULL
   OR privacy_level IS NULL;

CREATE TABLE IF NOT EXISTS support_responder_profiles (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    is_available_for_immediate BOOLEAN NOT NULL DEFAULT FALSE,
    is_available_for_community BOOLEAN NOT NULL DEFAULT TRUE,
    supports_chat BOOLEAN NOT NULL DEFAULT TRUE,
    supports_check_ins BOOLEAN NOT NULL DEFAULT TRUE,
    supports_in_person BOOLEAN NOT NULL DEFAULT FALSE,
    max_concurrent_sessions INTEGER NOT NULL DEFAULT 2,
    languages TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (max_concurrent_sessions >= 0 AND max_concurrent_sessions <= 10)
);

INSERT INTO support_responder_profiles (
    user_id,
    is_available_for_immediate,
    is_available_for_community
)
SELECT
    u.id,
    u.is_available_to_support,
    true
FROM users u
ON CONFLICT (user_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS support_responder_presence (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    is_active BOOLEAN NOT NULL DEFAULT FALSE,
    available_now BOOLEAN NOT NULL DEFAULT FALSE,
    active_session_count INTEGER NOT NULL DEFAULT 0,
    last_seen_at TIMESTAMP NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (active_session_count >= 0)
);

INSERT INTO support_responder_presence (user_id)
SELECT u.id
FROM users u
ON CONFLICT (user_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS support_responder_stats (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    acceptance_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    completion_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    helpfulness_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    median_response_seconds INTEGER NOT NULL DEFAULT 0,
    ignored_offer_count INTEGER NOT NULL DEFAULT 0,
    recent_decline_count INTEGER NOT NULL DEFAULT 0,
    total_completed_sessions INTEGER NOT NULL DEFAULT 0,
    total_cancelled_sessions INTEGER NOT NULL DEFAULT 0,
    last_session_completed_at TIMESTAMP NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO support_responder_stats (user_id)
SELECT u.id
FROM users u
ON CONFLICT (user_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS support_sessions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    support_request_id UUID NOT NULL REFERENCES support_requests(id) ON DELETE CASCADE,
    requester_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    responder_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active', 'completed', 'cancelled')),
    outcome TEXT NULL,
    chat_id UUID NULL REFERENCES chats(id) ON DELETE SET NULL,
    started_at TIMESTAMP NULL,
    completed_at TIMESTAMP NULL,
    cancelled_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE support_requests
    ADD CONSTRAINT support_requests_matched_session_id_fkey
        FOREIGN KEY (matched_session_id) REFERENCES support_sessions(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS support_offers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    support_request_id UUID NOT NULL REFERENCES support_requests(id) ON DELETE CASCADE,
    responder_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'declined', 'expired', 'closed')),
    match_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    fit_summary TEXT NULL,
    batch_number INTEGER NOT NULL DEFAULT 1,
    offered_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    responded_at TIMESTAMP NULL,
    closed_at TIMESTAMP NULL
);

CREATE TABLE IF NOT EXISTS support_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    support_request_id UUID NULL REFERENCES support_requests(id) ON DELETE CASCADE,
    support_offer_id UUID NULL REFERENCES support_offers(id) ON DELETE CASCADE,
    support_session_id UUID NULL REFERENCES support_sessions(id) ON DELETE CASCADE,
    actor_user_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
