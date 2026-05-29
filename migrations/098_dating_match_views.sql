CREATE TABLE IF NOT EXISTS dating_match_views (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO dating_match_views (user_id, seen_at, updated_at)
SELECT participant.user_id, NOW(), NOW()
FROM (
    SELECT user_a_id AS user_id FROM dating_matches WHERE status = 'active'
    UNION
    SELECT user_b_id AS user_id FROM dating_matches WHERE status = 'active'
) participant
ON CONFLICT (user_id) DO NOTHING;
