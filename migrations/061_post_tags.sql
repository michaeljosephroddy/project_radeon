CREATE TABLE IF NOT EXISTS post_tags (
    post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    tag TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (post_id, tag)
);

CREATE INDEX IF NOT EXISTS idx_post_tags_tag
    ON post_tags(tag);

CREATE INDEX IF NOT EXISTS idx_post_tags_post_id_created_at
    ON post_tags(post_id, created_at);
