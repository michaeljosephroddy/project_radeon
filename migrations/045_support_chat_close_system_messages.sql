ALTER TABLE messages
ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'user'
    CHECK (kind IN ('user', 'system'));
