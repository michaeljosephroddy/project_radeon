CREATE TABLE IF NOT EXISTS notification_counters (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    unread_count INTEGER NOT NULL DEFAULT 0 CHECK (unread_count >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO notification_counters (user_id, unread_count, updated_at)
SELECT user_id, COUNT(*)::int, NOW()
FROM notifications
WHERE read_at IS NULL
GROUP BY user_id
ON CONFLICT (user_id) DO UPDATE
SET unread_count = EXCLUDED.unread_count,
    updated_at = NOW();
