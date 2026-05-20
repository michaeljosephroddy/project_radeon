CREATE INDEX IF NOT EXISTS idx_dating_actions_target_like_updated
    ON dating_actions(target_id, action, updated_at DESC, id DESC)
    WHERE action = 'like';
