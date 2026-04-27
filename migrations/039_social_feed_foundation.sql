CREATE TABLE IF NOT EXISTS post_shares (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    commentary TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_post_shares_user_created_at
    ON post_shares(user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_post_shares_post_created_at
    ON post_shares(post_id, created_at DESC);

CREATE TABLE IF NOT EXISTS feed_hidden_posts (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id UUID NOT NULL,
    item_kind TEXT NOT NULL CHECK (item_kind IN ('post', 'reshare')),
    hidden_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, item_id, item_kind)
);

CREATE INDEX IF NOT EXISTS idx_feed_hidden_posts_user_hidden_at
    ON feed_hidden_posts(user_id, hidden_at DESC);

CREATE TABLE IF NOT EXISTS feed_muted_authors (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    author_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    muted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, author_id),
    CHECK (user_id <> author_id)
);

CREATE INDEX IF NOT EXISTS idx_feed_muted_authors_user_muted_at
    ON feed_muted_authors(user_id, muted_at DESC);

CREATE TABLE IF NOT EXISTS feed_impressions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id UUID NOT NULL,
    item_kind TEXT NOT NULL CHECK (item_kind IN ('post', 'reshare')),
    feed_mode TEXT NOT NULL CHECK (feed_mode IN ('friends', 'for_you')),
    session_id TEXT NOT NULL DEFAULT '',
    position INT NOT NULL DEFAULT 0,
    served_at TIMESTAMPTZ NOT NULL,
    viewed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    view_ms INT NOT NULL DEFAULT 0 CHECK (view_ms >= 0),
    was_clicked BOOLEAN NOT NULL DEFAULT FALSE,
    was_liked BOOLEAN NOT NULL DEFAULT FALSE,
    was_commented BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_feed_impressions_user_viewed_at
    ON feed_impressions(user_id, viewed_at DESC);

CREATE INDEX IF NOT EXISTS idx_feed_impressions_item_viewed_at
    ON feed_impressions(item_id, item_kind, viewed_at DESC);

CREATE TABLE IF NOT EXISTS feed_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id UUID NOT NULL,
    item_kind TEXT NOT NULL CHECK (item_kind IN ('post', 'reshare')),
    feed_mode TEXT NOT NULL CHECK (feed_mode IN ('friends', 'for_you')),
    event_type TEXT NOT NULL CHECK (
        event_type IN (
            'impression',
            'open_post',
            'open_comments',
            'comment',
            'like',
            'unlike',
            'share_open',
            'share_create',
            'hide',
            'mute_author'
        )
    ),
    position INT,
    event_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_feed_events_user_event_at
    ON feed_events(user_id, event_at DESC);

CREATE INDEX IF NOT EXISTS idx_feed_events_item_event_at
    ON feed_events(item_id, item_kind, event_at DESC);

CREATE TABLE IF NOT EXISTS post_stats_daily (
    post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    bucket_date DATE NOT NULL,
    impression_count INT NOT NULL DEFAULT 0,
    unique_viewer_count INT NOT NULL DEFAULT 0,
    like_count INT NOT NULL DEFAULT 0,
    comment_count INT NOT NULL DEFAULT 0,
    share_count INT NOT NULL DEFAULT 0,
    hide_count INT NOT NULL DEFAULT 0,
    report_count INT NOT NULL DEFAULT 0,
    PRIMARY KEY (post_id, bucket_date)
);

CREATE INDEX IF NOT EXISTS idx_post_stats_daily_bucket_date
    ON post_stats_daily(bucket_date DESC);

CREATE TABLE IF NOT EXISTS post_quality_features (
    post_id UUID PRIMARY KEY REFERENCES posts(id) ON DELETE CASCADE,
    author_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    has_body BOOLEAN NOT NULL DEFAULT FALSE,
    has_image BOOLEAN NOT NULL DEFAULT FALSE,
    body_length INT NOT NULL DEFAULT 0,
    recent_impression_count INT NOT NULL DEFAULT 0,
    recent_like_count INT NOT NULL DEFAULT 0,
    recent_comment_count INT NOT NULL DEFAULT 0,
    recent_share_count INT NOT NULL DEFAULT 0,
    recent_hide_count INT NOT NULL DEFAULT 0,
    quality_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_post_quality_features_author_id
    ON post_quality_features(author_id);

CREATE TABLE IF NOT EXISTS author_feed_stats (
    author_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    recent_post_count INT NOT NULL DEFAULT 0,
    recent_share_count INT NOT NULL DEFAULT 0,
    rolling_impression_count INT NOT NULL DEFAULT 0,
    rolling_like_count INT NOT NULL DEFAULT 0,
    rolling_comment_count INT NOT NULL DEFAULT 0,
    rolling_hide_count INT NOT NULL DEFAULT 0,
    last_post_at TIMESTAMPTZ,
    last_share_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
