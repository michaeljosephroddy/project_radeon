ALTER TABLE connections
    ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'FRIEND'
        CHECK (type IN ('FRIEND', 'MATCH'));

-- Enforce one connection per user pair at the DB level.
-- Required to safely handle the race where two users mutually like at the same time.
CREATE UNIQUE INDEX IF NOT EXISTS connections_pair_unique ON connections (requester_id, addressee_id);

CREATE TABLE IF NOT EXISTS likes (
    liker_id   UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    liked_id   UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (liker_id, liked_id)
);

CREATE INDEX IF NOT EXISTS idx_likes_liked_id ON likes(liked_id);
