CREATE TABLE IF NOT EXISTS group_posts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    post_type TEXT NOT NULL DEFAULT 'standard',
    body TEXT NOT NULL,
    anonymous BOOLEAN NOT NULL DEFAULT FALSE,
    pinned_at TIMESTAMPTZ,
    pinned_by UUID REFERENCES users(id) ON DELETE SET NULL,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    comment_count INT NOT NULL DEFAULT 0,
    reaction_count INT NOT NULL DEFAULT 0,
    image_count INT NOT NULL DEFAULT 0,
    CONSTRAINT group_posts_post_type_chk CHECK (post_type IN ('standard', 'milestone', 'need_support', 'admin_announcement', 'check_in')),
    CONSTRAINT group_posts_body_len_chk CHECK (char_length(body) BETWEEN 1 AND 4000)
);

CREATE INDEX IF NOT EXISTS idx_group_posts_group_pinned_created
    ON group_posts(group_id, pinned_at DESC NULLS LAST, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_group_posts_user_created
    ON group_posts(user_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS group_post_images (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    post_id UUID NOT NULL REFERENCES group_posts(id) ON DELETE CASCADE,
    image_url TEXT NOT NULL,
    thumb_url TEXT,
    width INT NOT NULL,
    height INT NOT NULL,
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT group_post_images_dimensions_chk CHECK (width > 0 AND height > 0)
);

CREATE INDEX IF NOT EXISTS idx_group_post_images_group_created
    ON group_post_images(group_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_group_post_images_post_position
    ON group_post_images(post_id, position);

CREATE TABLE IF NOT EXISTS group_comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    post_id UUID NOT NULL REFERENCES group_posts(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT group_comments_body_len_chk CHECK (char_length(body) BETWEEN 1 AND 2000)
);

CREATE INDEX IF NOT EXISTS idx_group_comments_post_created
    ON group_comments(post_id, created_at)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_group_comments_user_created
    ON group_comments(user_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS group_reactions (
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    post_id UUID NOT NULL REFERENCES group_posts(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type TEXT NOT NULL DEFAULT 'like',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (post_id, user_id, type),
    CONSTRAINT group_reactions_type_chk CHECK (type IN ('like'))
);

CREATE INDEX IF NOT EXISTS idx_group_reactions_group_post
    ON group_reactions(group_id, post_id);
