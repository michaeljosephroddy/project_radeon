CREATE INDEX IF NOT EXISTS idx_comments_post_id_created_at ON comments(post_id, created_at);
CREATE INDEX IF NOT EXISTS idx_comments_user_id ON comments(user_id);
