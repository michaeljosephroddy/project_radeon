CREATE INDEX IF NOT EXISTS idx_feed_muted_authors_user_muted_author
    ON feed_muted_authors(user_id, muted_at DESC, author_id DESC);
