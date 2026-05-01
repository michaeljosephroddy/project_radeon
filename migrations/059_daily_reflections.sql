CREATE TABLE IF NOT EXISTS daily_reflections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reflection_date DATE NOT NULL,
    prompt_key TEXT NULL,
    prompt_text TEXT NULL,
    body TEXT NOT NULL,
    shared_post_id UUID NULL REFERENCES posts(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, reflection_date),
    CHECK (length(body) <= 2000)
);

CREATE INDEX IF NOT EXISTS idx_daily_reflections_user_date_desc
    ON daily_reflections(user_id, reflection_date DESC);

ALTER TABLE posts
    ADD COLUMN IF NOT EXISTS source_type TEXT NULL,
    ADD COLUMN IF NOT EXISTS source_id UUID NULL,
    ADD COLUMN IF NOT EXISTS source_label TEXT NULL;

ALTER TABLE posts
    DROP CONSTRAINT IF EXISTS posts_source_type_chk;

ALTER TABLE posts
    ADD CONSTRAINT posts_source_type_chk
    CHECK (source_type IS NULL OR source_type IN ('daily_reflection'));
