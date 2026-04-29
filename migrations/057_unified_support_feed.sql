ALTER TABLE support_requests
    ADD COLUMN IF NOT EXISTS support_type TEXT,
    ADD COLUMN IF NOT EXISTS topics TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS preferred_gender TEXT NULL,
    ADD COLUMN IF NOT EXISTS location_visibility TEXT NOT NULL DEFAULT 'hidden',
    ADD COLUMN IF NOT EXISTS location_city TEXT NULL,
    ADD COLUMN IF NOT EXISTS location_region TEXT NULL,
    ADD COLUMN IF NOT EXISTS location_country TEXT NULL,
    ADD COLUMN IF NOT EXISTS location_approx_lat DOUBLE PRECISION NULL,
    ADD COLUMN IF NOT EXISTS location_approx_lng DOUBLE PRECISION NULL,
    ADD COLUMN IF NOT EXISTS reply_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS view_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS is_priority BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS priority_expires_at TIMESTAMPTZ NULL;

UPDATE support_requests
SET support_type = CASE type
    WHEN 'need_to_talk' THEN 'chat'
    WHEN 'need_in_person_help' THEN 'meetup'
    WHEN 'need_distraction' THEN 'general'
    WHEN 'need_encouragement' THEN 'general'
    ELSE 'general'
END
WHERE support_type IS NULL OR support_type = '';

UPDATE support_requests
SET location_city = COALESCE(location_city, city),
    location_visibility = CASE
        WHEN location_visibility = 'hidden' AND city IS NOT NULL AND city <> '' THEN 'city'
        ELSE location_visibility
    END
WHERE city IS NOT NULL AND city <> '';

ALTER TABLE support_requests
    ALTER COLUMN support_type SET DEFAULT 'general',
    ALTER COLUMN support_type SET NOT NULL;

ALTER TABLE support_requests
    DROP CONSTRAINT IF EXISTS support_requests_support_type_check,
    DROP CONSTRAINT IF EXISTS support_requests_type_check,
    DROP CONSTRAINT IF EXISTS support_requests_urgency_check,
    DROP CONSTRAINT IF EXISTS support_requests_preferred_gender_check,
    DROP CONSTRAINT IF EXISTS support_requests_location_visibility_check;

UPDATE support_requests
SET urgency = CASE urgency
    WHEN 'right_now' THEN 'high'
    WHEN 'soon' THEN 'medium'
    WHEN 'when_you_can' THEN 'low'
    WHEN 'high' THEN 'high'
    WHEN 'medium' THEN 'medium'
    ELSE 'low'
END;

ALTER TABLE support_requests
    ALTER COLUMN urgency SET DEFAULT 'low';

ALTER TABLE support_requests
    ADD CONSTRAINT support_requests_type_check
        CHECK (type IN ('need_to_talk', 'need_distraction', 'need_encouragement', 'need_in_person_help', 'chat', 'call', 'meetup', 'general')),
    ADD CONSTRAINT support_requests_support_type_check
        CHECK (support_type IN ('chat', 'call', 'meetup', 'general')),
    ADD CONSTRAINT support_requests_urgency_check
        CHECK (urgency IN ('low', 'medium', 'high')),
    ADD CONSTRAINT support_requests_preferred_gender_check
        CHECK (preferred_gender IS NULL OR preferred_gender IN ('woman', 'man', 'non_binary', 'no_preference')),
    ADD CONSTRAINT support_requests_location_visibility_check
        CHECK (location_visibility IN ('hidden', 'city', 'approximate'));

ALTER TABLE support_responses
    DROP CONSTRAINT IF EXISTS support_responses_response_type_check;

UPDATE support_responses
SET response_type = CASE response_type
    WHEN 'can_chat' THEN 'chat'
    WHEN 'can_meet' THEN 'meetup'
    WHEN 'check_in_later' THEN 'chat'
    WHEN 'call' THEN 'call'
    WHEN 'meetup' THEN 'meetup'
    ELSE 'chat'
END;

ALTER TABLE support_responses
    ADD CONSTRAINT support_responses_response_type_check
        CHECK (response_type IN ('chat', 'call', 'meetup'));

CREATE TABLE IF NOT EXISTS support_replies (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    support_request_id UUID NOT NULL REFERENCES support_requests(id) ON DELETE CASCADE,
    author_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_support_replies_request_created_id
    ON support_replies(support_request_id, created_at ASC, id ASC);

CREATE INDEX IF NOT EXISTS idx_support_replies_author_created_at
    ON support_replies(author_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_requests_unified_open_feed
    ON support_requests(status, urgency, created_at DESC, id DESC)
    WHERE status = 'open';

CREATE INDEX IF NOT EXISTS idx_support_requests_support_type_open
    ON support_requests(support_type, created_at DESC, id DESC)
    WHERE status = 'open';

CREATE INDEX IF NOT EXISTS idx_support_requests_topics_gin
    ON support_requests USING GIN (topics);

CREATE INDEX IF NOT EXISTS idx_support_requests_priority_open
    ON support_requests(priority_expires_at DESC)
    WHERE status = 'open' AND is_priority = TRUE;
