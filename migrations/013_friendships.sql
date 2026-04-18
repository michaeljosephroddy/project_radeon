CREATE TABLE IF NOT EXISTS friendships (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_a_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_b_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    requester_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('pending', 'accepted')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    accepted_at TIMESTAMPTZ NULL,
    CHECK (user_a_id <> user_b_id),
    CHECK (requester_id = user_a_id OR requester_id = user_b_id),
    UNIQUE (user_a_id, user_b_id)
);

CREATE INDEX IF NOT EXISTS friendships_user_a_id_idx ON friendships(user_a_id);
CREATE INDEX IF NOT EXISTS friendships_user_b_id_idx ON friendships(user_b_id);
CREATE INDEX IF NOT EXISTS friendships_requester_id_idx ON friendships(requester_id);
CREATE INDEX IF NOT EXISTS friendships_status_idx ON friendships(status);

INSERT INTO friendships (
    user_a_id,
    user_b_id,
    requester_id,
    status,
    created_at,
    accepted_at
)
SELECT
    CASE
        WHEN f1.follower_id::text < f1.following_id::text THEN f1.follower_id
        ELSE f1.following_id
    END AS user_a_id,
    CASE
        WHEN f1.follower_id::text < f1.following_id::text THEN f1.following_id
        ELSE f1.follower_id
    END AS user_b_id,
    CASE
        WHEN f1.follower_id::text < f1.following_id::text THEN f1.follower_id
        ELSE f1.following_id
    END AS requester_id,
    'accepted',
    LEAST(f1.created_at, f2.created_at),
    GREATEST(f1.created_at, f2.created_at)
FROM follows f1
JOIN follows f2
    ON f1.follower_id = f2.following_id
    AND f1.following_id = f2.follower_id
WHERE f1.follower_id::text < f1.following_id::text
ON CONFLICT (user_a_id, user_b_id) DO NOTHING;

UPDATE support_requests
SET audience = 'friends'
WHERE audience = 'followers';

ALTER TABLE support_requests
DROP CONSTRAINT IF EXISTS support_requests_audience_check;

ALTER TABLE support_requests
ADD CONSTRAINT support_requests_audience_check
CHECK (audience IN ('friends', 'city', 'community'));
