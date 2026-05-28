CREATE INDEX IF NOT EXISTS idx_user_blocks_blocker_created
    ON user_blocks(blocker_id, created_at DESC, blocked_id DESC);
