CREATE UNIQUE INDEX IF NOT EXISTS idx_feed_impressions_session_item_served
    ON feed_impressions(user_id, item_id, item_kind, feed_mode, session_id, served_at);

ALTER TABLE post_quality_features
    ADD COLUMN IF NOT EXISTS total_impression_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_like_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_comment_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_share_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_hide_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_engagement_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS share_reactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    share_id UUID NOT NULL REFERENCES post_shares(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (share_id, user_id, type)
);

CREATE INDEX IF NOT EXISTS idx_share_reactions_share_id_type
    ON share_reactions(share_id, type);

CREATE INDEX IF NOT EXISTS idx_share_reactions_user_created_at
    ON share_reactions(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS share_comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    share_id UUID NOT NULL REFERENCES post_shares(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_share_comments_share_id_created_at
    ON share_comments(share_id, created_at);

CREATE INDEX IF NOT EXISTS idx_share_comments_user_id
    ON share_comments(user_id);

CREATE TABLE IF NOT EXISTS share_comment_mentions (
    share_comment_id UUID NOT NULL REFERENCES share_comments(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (share_comment_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_share_comment_mentions_user_id
    ON share_comment_mentions(user_id);

CREATE TABLE IF NOT EXISTS share_quality_features (
    share_id UUID PRIMARY KEY REFERENCES post_shares(id) ON DELETE CASCADE,
    author_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    original_post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    has_commentary BOOLEAN NOT NULL DEFAULT FALSE,
    commentary_length INT NOT NULL DEFAULT 0,
    total_impression_count INT NOT NULL DEFAULT 0,
    total_like_count INT NOT NULL DEFAULT 0,
    total_comment_count INT NOT NULL DEFAULT 0,
    total_hide_count INT NOT NULL DEFAULT 0,
    recent_impression_count INT NOT NULL DEFAULT 0,
    recent_like_count INT NOT NULL DEFAULT 0,
    recent_comment_count INT NOT NULL DEFAULT 0,
    recent_hide_count INT NOT NULL DEFAULT 0,
    quality_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_engagement_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_share_quality_features_author_id
    ON share_quality_features(author_id);

INSERT INTO post_quality_features (
    post_id,
    author_id,
    has_body,
    has_image,
    body_length,
    total_impression_count,
    total_like_count,
    total_comment_count,
    total_share_count,
    total_hide_count,
    recent_impression_count,
    recent_like_count,
    recent_comment_count,
    recent_share_count,
    recent_hide_count,
    quality_score,
    last_engagement_at,
    updated_at
)
SELECT
    p.id,
    p.user_id,
    COALESCE(NULLIF(BTRIM(p.body), ''), '') <> '' AS has_body,
    EXISTS(SELECT 1 FROM post_images pi WHERE pi.post_id = p.id) AS has_image,
    CHAR_LENGTH(COALESCE(p.body, '')) AS body_length,
    COALESCE(total_impressions.cnt, 0) AS total_impression_count,
    COALESCE(total_likes.cnt, 0) AS total_like_count,
    COALESCE(total_comments.cnt, 0) AS total_comment_count,
    COALESCE(total_shares.cnt, 0) AS total_share_count,
    COALESCE(total_hides.cnt, 0) AS total_hide_count,
    COALESCE(recent_impressions.cnt, 0) AS recent_impression_count,
    COALESCE(total_likes.cnt, 0) AS recent_like_count,
    COALESCE(recent_comments.cnt, 0) AS recent_comment_count,
    COALESCE(recent_shares.cnt, 0) AS recent_share_count,
    COALESCE(recent_hides.cnt, 0) AS recent_hide_count,
    (
        LEAST(COALESCE(total_likes.cnt, 0), 100) * 0.8
        + LEAST(COALESCE(total_comments.cnt, 0), 100) * 1.3
        + LEAST(COALESCE(total_shares.cnt, 0), 100) * 1.1
        + CASE
            WHEN EXISTS(SELECT 1 FROM post_images pi WHERE pi.post_id = p.id) THEN 4
            ELSE 0
        END
        - LEAST(COALESCE(total_hides.cnt, 0), 100) * 2.0
    )::double precision AS quality_score,
    GREATEST(
        p.created_at,
        COALESCE(total_comments.last_at, p.created_at),
        COALESCE(total_shares.last_at, p.created_at),
        COALESCE(total_impressions.last_at, p.created_at),
        COALESCE(total_hides.last_at, p.created_at)
    ) AS last_engagement_at,
    NOW()
FROM posts p
LEFT JOIN (
    SELECT item_id AS post_id, COUNT(*)::int AS cnt, MAX(viewed_at) AS last_at
    FROM feed_impressions
    WHERE item_kind = 'post'
    GROUP BY item_id
) total_impressions ON total_impressions.post_id = p.id
LEFT JOIN (
    SELECT item_id AS post_id, COUNT(*)::int AS cnt
    FROM feed_impressions
    WHERE item_kind = 'post' AND viewed_at >= NOW() - INTERVAL '14 days'
    GROUP BY item_id
) recent_impressions ON recent_impressions.post_id = p.id
LEFT JOIN (
    SELECT post_id, COUNT(*)::int AS cnt
    FROM post_reactions
    WHERE type = 'like'
    GROUP BY post_id
) total_likes ON total_likes.post_id = p.id
LEFT JOIN (
    SELECT post_id, COUNT(*)::int AS cnt, MAX(created_at) AS last_at
    FROM comments
    GROUP BY post_id
) total_comments ON total_comments.post_id = p.id
LEFT JOIN (
    SELECT post_id, COUNT(*)::int AS cnt
    FROM comments
    WHERE created_at >= NOW() - INTERVAL '14 days'
    GROUP BY post_id
) recent_comments ON recent_comments.post_id = p.id
LEFT JOIN (
    SELECT post_id, COUNT(*)::int AS cnt, MAX(created_at) AS last_at
    FROM post_shares
    GROUP BY post_id
) total_shares ON total_shares.post_id = p.id
LEFT JOIN (
    SELECT post_id, COUNT(*)::int AS cnt
    FROM post_shares
    WHERE created_at >= NOW() - INTERVAL '14 days'
    GROUP BY post_id
) recent_shares ON recent_shares.post_id = p.id
LEFT JOIN (
    SELECT item_id AS post_id, COUNT(*)::int AS cnt, MAX(hidden_at) AS last_at
    FROM feed_hidden_posts
    WHERE item_kind = 'post'
    GROUP BY item_id
) total_hides ON total_hides.post_id = p.id
LEFT JOIN (
    SELECT item_id AS post_id, COUNT(*)::int AS cnt
    FROM feed_hidden_posts
    WHERE item_kind = 'post' AND hidden_at >= NOW() - INTERVAL '14 days'
    GROUP BY item_id
) recent_hides ON recent_hides.post_id = p.id
ON CONFLICT (post_id) DO UPDATE SET
    author_id = EXCLUDED.author_id,
    has_body = EXCLUDED.has_body,
    has_image = EXCLUDED.has_image,
    body_length = EXCLUDED.body_length,
    total_impression_count = EXCLUDED.total_impression_count,
    total_like_count = EXCLUDED.total_like_count,
    total_comment_count = EXCLUDED.total_comment_count,
    total_share_count = EXCLUDED.total_share_count,
    total_hide_count = EXCLUDED.total_hide_count,
    recent_impression_count = EXCLUDED.recent_impression_count,
    recent_like_count = EXCLUDED.recent_like_count,
    recent_comment_count = EXCLUDED.recent_comment_count,
    recent_share_count = EXCLUDED.recent_share_count,
    recent_hide_count = EXCLUDED.recent_hide_count,
    quality_score = EXCLUDED.quality_score,
    last_engagement_at = EXCLUDED.last_engagement_at,
    updated_at = NOW();

INSERT INTO share_quality_features (
    share_id,
    author_id,
    original_post_id,
    has_commentary,
    commentary_length,
    total_impression_count,
    total_like_count,
    total_comment_count,
    total_hide_count,
    recent_impression_count,
    recent_like_count,
    recent_comment_count,
    recent_hide_count,
    quality_score,
    last_engagement_at,
    updated_at
)
SELECT
    ps.id,
    ps.user_id,
    ps.post_id,
    COALESCE(NULLIF(BTRIM(ps.commentary), ''), '') <> '' AS has_commentary,
    CHAR_LENGTH(COALESCE(ps.commentary, '')) AS commentary_length,
    COALESCE(total_impressions.cnt, 0) AS total_impression_count,
    COALESCE(total_likes.cnt, 0) AS total_like_count,
    COALESCE(total_comments.cnt, 0) AS total_comment_count,
    COALESCE(total_hides.cnt, 0) AS total_hide_count,
    COALESCE(recent_impressions.cnt, 0) AS recent_impression_count,
    COALESCE(recent_likes.cnt, 0) AS recent_like_count,
    COALESCE(recent_comments.cnt, 0) AS recent_comment_count,
    COALESCE(recent_hides.cnt, 0) AS recent_hide_count,
    (
        LEAST(COALESCE(total_likes.cnt, 0), 100) * 0.8
        + LEAST(COALESCE(total_comments.cnt, 0), 100) * 1.1
        + CASE
            WHEN COALESCE(NULLIF(BTRIM(ps.commentary), ''), '') <> '' THEN 3
            ELSE 0
        END
        - LEAST(COALESCE(total_hides.cnt, 0), 100) * 1.8
    )::double precision AS quality_score,
    GREATEST(
        ps.created_at,
        COALESCE(total_comments.last_at, ps.created_at),
        COALESCE(total_likes.last_at, ps.created_at),
        COALESCE(total_impressions.last_at, ps.created_at),
        COALESCE(total_hides.last_at, ps.created_at)
    ) AS last_engagement_at,
    NOW()
FROM post_shares ps
LEFT JOIN (
    SELECT item_id AS share_id, COUNT(*)::int AS cnt, MAX(viewed_at) AS last_at
    FROM feed_impressions
    WHERE item_kind = 'reshare'
    GROUP BY item_id
) total_impressions ON total_impressions.share_id = ps.id
LEFT JOIN (
    SELECT item_id AS share_id, COUNT(*)::int AS cnt
    FROM feed_impressions
    WHERE item_kind = 'reshare' AND viewed_at >= NOW() - INTERVAL '14 days'
    GROUP BY item_id
) recent_impressions ON recent_impressions.share_id = ps.id
LEFT JOIN (
    SELECT share_id, COUNT(*)::int AS cnt, MAX(created_at) AS last_at
    FROM share_reactions
    WHERE type = 'like'
    GROUP BY share_id
) total_likes ON total_likes.share_id = ps.id
LEFT JOIN (
    SELECT share_id, COUNT(*)::int AS cnt
    FROM share_reactions
    WHERE type = 'like' AND created_at >= NOW() - INTERVAL '14 days'
    GROUP BY share_id
) recent_likes ON recent_likes.share_id = ps.id
LEFT JOIN (
    SELECT share_id, COUNT(*)::int AS cnt, MAX(created_at) AS last_at
    FROM share_comments
    GROUP BY share_id
) total_comments ON total_comments.share_id = ps.id
LEFT JOIN (
    SELECT share_id, COUNT(*)::int AS cnt
    FROM share_comments
    WHERE created_at >= NOW() - INTERVAL '14 days'
    GROUP BY share_id
) recent_comments ON recent_comments.share_id = ps.id
LEFT JOIN (
    SELECT item_id AS share_id, COUNT(*)::int AS cnt, MAX(hidden_at) AS last_at
    FROM feed_hidden_posts
    WHERE item_kind = 'reshare'
    GROUP BY item_id
) total_hides ON total_hides.share_id = ps.id
LEFT JOIN (
    SELECT item_id AS share_id, COUNT(*)::int AS cnt
    FROM feed_hidden_posts
    WHERE item_kind = 'reshare' AND hidden_at >= NOW() - INTERVAL '14 days'
    GROUP BY item_id
) recent_hides ON recent_hides.share_id = ps.id
ON CONFLICT (share_id) DO UPDATE SET
    author_id = EXCLUDED.author_id,
    original_post_id = EXCLUDED.original_post_id,
    has_commentary = EXCLUDED.has_commentary,
    commentary_length = EXCLUDED.commentary_length,
    total_impression_count = EXCLUDED.total_impression_count,
    total_like_count = EXCLUDED.total_like_count,
    total_comment_count = EXCLUDED.total_comment_count,
    total_hide_count = EXCLUDED.total_hide_count,
    recent_impression_count = EXCLUDED.recent_impression_count,
    recent_like_count = EXCLUDED.recent_like_count,
    recent_comment_count = EXCLUDED.recent_comment_count,
    recent_hide_count = EXCLUDED.recent_hide_count,
    quality_score = EXCLUDED.quality_score,
    last_engagement_at = EXCLUDED.last_engagement_at,
    updated_at = NOW();

INSERT INTO author_feed_stats (
    author_id,
    recent_post_count,
    recent_share_count,
    rolling_impression_count,
    rolling_like_count,
    rolling_comment_count,
    rolling_hide_count,
    last_post_at,
    last_share_at,
    updated_at
)
SELECT
    u.id,
    COALESCE(posts_14d.cnt, 0) AS recent_post_count,
    COALESCE(shares_14d.cnt, 0) AS recent_share_count,
    COALESCE(post_impressions.cnt, 0) + COALESCE(share_impressions.cnt, 0) AS rolling_impression_count,
    COALESCE(post_likes.cnt, 0) + COALESCE(share_likes.cnt, 0) AS rolling_like_count,
    COALESCE(post_comments.cnt, 0) + COALESCE(share_comments.cnt, 0) AS rolling_comment_count,
    COALESCE(post_hides.cnt, 0) + COALESCE(share_hides.cnt, 0) AS rolling_hide_count,
    posts_last.last_at AS last_post_at,
    shares_last.last_at AS last_share_at,
    NOW()
FROM users u
LEFT JOIN (
    SELECT user_id, COUNT(*)::int AS cnt
    FROM posts
    WHERE created_at >= NOW() - INTERVAL '14 days'
    GROUP BY user_id
) posts_14d ON posts_14d.user_id = u.id
LEFT JOIN (
    SELECT user_id, MAX(created_at) AS last_at
    FROM posts
    GROUP BY user_id
) posts_last ON posts_last.user_id = u.id
LEFT JOIN (
    SELECT user_id, COUNT(*)::int AS cnt
    FROM post_shares
    WHERE created_at >= NOW() - INTERVAL '14 days'
    GROUP BY user_id
) shares_14d ON shares_14d.user_id = u.id
LEFT JOIN (
    SELECT user_id, MAX(created_at) AS last_at
    FROM post_shares
    GROUP BY user_id
) shares_last ON shares_last.user_id = u.id
LEFT JOIN (
    SELECT p.user_id, COUNT(*)::int AS cnt
    FROM posts p
    JOIN feed_impressions fi ON fi.item_id = p.id AND fi.item_kind = 'post'
    WHERE fi.viewed_at >= NOW() - INTERVAL '30 days'
    GROUP BY p.user_id
) post_impressions ON post_impressions.user_id = u.id
LEFT JOIN (
    SELECT ps.user_id, COUNT(*)::int AS cnt
    FROM post_shares ps
    JOIN feed_impressions fi ON fi.item_id = ps.id AND fi.item_kind = 'reshare'
    WHERE fi.viewed_at >= NOW() - INTERVAL '30 days'
    GROUP BY ps.user_id
) share_impressions ON share_impressions.user_id = u.id
LEFT JOIN (
    SELECT p.user_id, COUNT(*)::int AS cnt
    FROM posts p
    JOIN post_reactions pr ON pr.post_id = p.id AND pr.type = 'like'
    GROUP BY p.user_id
) post_likes ON post_likes.user_id = u.id
LEFT JOIN (
    SELECT ps.user_id, COUNT(*)::int AS cnt
    FROM post_shares ps
    JOIN share_reactions sr ON sr.share_id = ps.id AND sr.type = 'like'
    GROUP BY ps.user_id
) share_likes ON share_likes.user_id = u.id
LEFT JOIN (
    SELECT p.user_id, COUNT(*)::int AS cnt
    FROM posts p
    JOIN comments c ON c.post_id = p.id
    GROUP BY p.user_id
) post_comments ON post_comments.user_id = u.id
LEFT JOIN (
    SELECT ps.user_id, COUNT(*)::int AS cnt
    FROM post_shares ps
    JOIN share_comments sc ON sc.share_id = ps.id
    GROUP BY ps.user_id
) share_comments ON share_comments.user_id = u.id
LEFT JOIN (
    SELECT p.user_id, COUNT(*)::int AS cnt
    FROM posts p
    JOIN feed_hidden_posts fh ON fh.item_id = p.id AND fh.item_kind = 'post'
    GROUP BY p.user_id
) post_hides ON post_hides.user_id = u.id
LEFT JOIN (
    SELECT ps.user_id, COUNT(*)::int AS cnt
    FROM post_shares ps
    JOIN feed_hidden_posts fh ON fh.item_id = ps.id AND fh.item_kind = 'reshare'
    GROUP BY ps.user_id
) share_hides ON share_hides.user_id = u.id
ON CONFLICT (author_id) DO UPDATE SET
    recent_post_count = EXCLUDED.recent_post_count,
    recent_share_count = EXCLUDED.recent_share_count,
    rolling_impression_count = EXCLUDED.rolling_impression_count,
    rolling_like_count = EXCLUDED.rolling_like_count,
    rolling_comment_count = EXCLUDED.rolling_comment_count,
    rolling_hide_count = EXCLUDED.rolling_hide_count,
    last_post_at = EXCLUDED.last_post_at,
    last_share_at = EXCLUDED.last_share_at,
    updated_at = NOW();
