ALTER TABLE users
    ADD COLUMN IF NOT EXISTS connection_intents TEXT[] NOT NULL DEFAULT ARRAY['support', 'friends']::TEXT[];

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_connection_intents_chk;

ALTER TABLE users
    ADD CONSTRAINT users_connection_intents_chk
    CHECK (
        cardinality(connection_intents) BETWEEN 1 AND 3
        AND connection_intents <@ ARRAY['support', 'friends', 'dating']::TEXT[]
    );

CREATE INDEX IF NOT EXISTS idx_users_connection_intents
    ON users USING GIN(connection_intents);

CREATE TABLE IF NOT EXISTS user_blocks (
    blocker_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (blocker_id, blocked_id),
    CHECK (blocker_id <> blocked_id)
);

CREATE INDEX IF NOT EXISTS idx_user_blocks_blocked_id
    ON user_blocks(blocked_id);

CREATE TABLE IF NOT EXISTS user_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reported_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason TEXT NOT NULL,
    details TEXT,
    status TEXT NOT NULL DEFAULT 'open',
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (reporter_id <> reported_user_id),
    CONSTRAINT user_reports_reason_chk CHECK (reason IN ('unwanted_advances', 'harassment', 'spam', 'safety_concern', 'other')),
    CONSTRAINT user_reports_status_chk CHECK (status IN ('open', 'reviewing', 'resolved', 'dismissed'))
);

CREATE INDEX IF NOT EXISTS idx_user_reports_reported_status_created
    ON user_reports(reported_user_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_user_reports_reporter_created
    ON user_reports(reporter_id, created_at DESC);
