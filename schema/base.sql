CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username TEXT NOT NULL,
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL DEFAULT '',
    avatar_url TEXT,
    city TEXT,
    country TEXT,
    bio TEXT,
    gender TEXT,
    birth_date DATE,
    sober_since DATE,
    subscription_tier TEXT NOT NULL DEFAULT 'free',
    subscription_status TEXT NOT NULL DEFAULT 'inactive',
    friend_count INT NOT NULL DEFAULT 0,
    lat DOUBLE PRECISION,
    lng DOUBLE PRECISION,
    current_lat DOUBLE PRECISION,
    current_lng DOUBLE PRECISION,
    current_city TEXT,
    current_country TEXT,
    location_updated_at TIMESTAMPTZ,
    discover_lat DOUBLE PRECISION,
    discover_lng DOUBLE PRECISION,
    connection_intents TEXT[] NOT NULL DEFAULT ARRAY['friends']::TEXT[],
    onboarding_completed_at TIMESTAMPTZ,
    identity_verification_status TEXT NOT NULL DEFAULT 'not_started',
    identity_verification_provider TEXT,
    identity_verification_session_id TEXT,
    identity_verification_last_error TEXT,
    identity_verified_at TIMESTAMPTZ,
    onboarding_owner_welcome_comment_id UUID,
    sobriety_band SMALLINT,
    profile_completeness SMALLINT NOT NULL DEFAULT 0,
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT users_username_format_chk CHECK (username ~ '^[a-z0-9._]{3,20}$'),
    CONSTRAINT users_subscription_tier_chk CHECK (subscription_tier IN ('free', 'plus')),
    CONSTRAINT users_subscription_status_chk CHECK (subscription_status IN ('inactive', 'active', 'canceled', 'expired')),
    CONSTRAINT users_connection_intents_chk CHECK (
        cardinality(connection_intents) BETWEEN 1 AND 2
        AND connection_intents <@ ARRAY['friends', 'dating']::TEXT[]
        AND connection_intents @> ARRAY['friends']::TEXT[]
    ),
    CONSTRAINT users_identity_verification_status_chk CHECK (
        identity_verification_status IN (
            'not_started',
            'requires_input',
            'pending',
            'verified',
            'failed',
            'requires_retry'
        )
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS users_email_unique_idx
    ON users(email);

CREATE UNIQUE INDEX IF NOT EXISTS users_username_unique_idx
    ON users(username);

CREATE INDEX IF NOT EXISTS idx_users_username_trgm
    ON users USING GIN(username gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_users_city
    ON users(city);

CREATE INDEX IF NOT EXISTS idx_users_gender
    ON users(gender);

CREATE INDEX IF NOT EXISTS idx_users_last_active_at_desc
    ON users(last_active_at DESC);

CREATE INDEX IF NOT EXISTS idx_users_discover_lat_lng
    ON users(discover_lat, discover_lng);

CREATE INDEX IF NOT EXISTS idx_users_connection_intents
    ON users USING GIN(connection_intents);

CREATE INDEX IF NOT EXISTS idx_users_onboarding_completed_at
    ON users(onboarding_completed_at);

CREATE INDEX IF NOT EXISTS idx_users_identity_verification_status
    ON users(identity_verification_status);

CREATE INDEX IF NOT EXISTS idx_users_identity_verification_session_id
    ON users(identity_verification_session_id)
    WHERE identity_verification_session_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_users_deleted_at
    ON users(deleted_at);

CREATE TABLE IF NOT EXISTS interests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS user_interests (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    interest_id UUID NOT NULL REFERENCES interests(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, interest_id)
);

CREATE INDEX IF NOT EXISTS idx_user_interests_user_id
    ON user_interests(user_id);

CREATE INDEX IF NOT EXISTS idx_user_interests_interest_id
    ON user_interests(interest_id);

CREATE TABLE IF NOT EXISTS posts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body TEXT,
    source_type TEXT,
    source_id UUID,
    source_label TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS post_images (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    image_url TEXT NOT NULL,
    width INT NOT NULL,
    height INT NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_posts_user_id
    ON posts(user_id);

CREATE INDEX IF NOT EXISTS idx_posts_created_at
    ON posts(created_at);

CREATE INDEX IF NOT EXISTS idx_posts_created_at_desc
    ON posts(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_posts_user_id_created_at
    ON posts(user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_post_images_post_id_sort_order
    ON post_images(post_id, sort_order, created_at);

CREATE TABLE IF NOT EXISTS groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT,
    rules TEXT,
    avatar_url TEXT,
    cover_url TEXT,
    visibility TEXT NOT NULL DEFAULT 'public',
    posting_permission TEXT NOT NULL DEFAULT 'members',
    allow_anonymous_posts BOOLEAN NOT NULL DEFAULT FALSE,
    city TEXT,
    country TEXT,
    tags TEXT[] NOT NULL DEFAULT '{}',
    recovery_pathways TEXT[] NOT NULL DEFAULT '{}',
    member_count INT NOT NULL DEFAULT 0,
    post_count INT NOT NULL DEFAULT 0,
    media_count INT NOT NULL DEFAULT 0,
    pending_request_count INT NOT NULL DEFAULT 0,
    is_system BOOLEAN NOT NULL DEFAULT FALSE,
    system_key TEXT,
    locked_settings BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT groups_name_len_chk CHECK (char_length(name) BETWEEN 3 AND 80),
    CONSTRAINT groups_visibility_chk CHECK (visibility IN ('public', 'approval_required', 'invite_only', 'private_hidden')),
    CONSTRAINT groups_posting_permission_chk CHECK (posting_permission IN ('members', 'admins'))
);

CREATE UNIQUE INDEX IF NOT EXISTS groups_slug_unique_idx
    ON groups(slug)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_groups_visibility_created_at
    ON groups(visibility, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_groups_city_country
    ON groups(city, country)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_groups_tags_gin
    ON groups USING GIN(tags);

CREATE INDEX IF NOT EXISTS idx_groups_recovery_pathways_gin
    ON groups USING GIN(recovery_pathways);

CREATE INDEX IF NOT EXISTS idx_groups_name_trgm
    ON groups USING GIN(name gin_trgm_ops);

CREATE UNIQUE INDEX IF NOT EXISTS groups_system_key_unique_idx
    ON groups(system_key)
    WHERE system_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS group_memberships (
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member',
    status TEXT NOT NULL DEFAULT 'active',
    invited_by UUID REFERENCES users(id) ON DELETE SET NULL,
    joined_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, user_id),
    CONSTRAINT group_memberships_role_chk CHECK (role IN ('owner', 'admin', 'moderator', 'member')),
    CONSTRAINT group_memberships_status_chk CHECK (status IN ('active', 'banned')),
    CONSTRAINT group_memberships_joined_at_chk CHECK ((status = 'active' AND joined_at IS NOT NULL) OR status = 'banned')
);

CREATE INDEX IF NOT EXISTS idx_group_memberships_user_status_updated
    ON group_memberships(user_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_group_memberships_group_status_role
    ON group_memberships(group_id, status, role);

CREATE TABLE IF NOT EXISTS group_join_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT group_join_requests_status_chk CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS group_join_requests_pending_unique_idx
    ON group_join_requests(group_id, user_id)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_group_join_requests_group_status_created
    ON group_join_requests(group_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS group_invites (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ,
    max_uses INT,
    use_count INT NOT NULL DEFAULT 0,
    requires_approval BOOLEAN NOT NULL DEFAULT FALSE,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT group_invites_max_uses_chk CHECK (max_uses IS NULL OR max_uses > 0)
);

CREATE INDEX IF NOT EXISTS idx_group_invites_group_active
    ON group_invites(group_id, revoked_at, expires_at);

CREATE TABLE IF NOT EXISTS group_admin_threads (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'open',
    subject TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT group_admin_threads_status_chk CHECK (status IN ('open', 'replied', 'resolved'))
);

CREATE INDEX IF NOT EXISTS idx_group_admin_threads_group_status_updated
    ON group_admin_threads(group_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS group_admin_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    thread_id UUID NOT NULL REFERENCES group_admin_threads(id) ON DELETE CASCADE,
    sender_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT group_admin_messages_body_len_chk CHECK (char_length(body) BETWEEN 1 AND 2000)
);

CREATE INDEX IF NOT EXISTS idx_group_admin_messages_thread_created
    ON group_admin_messages(thread_id, created_at);

CREATE TABLE IF NOT EXISTS group_admin_thread_reads (
    thread_id UUID NOT NULL REFERENCES group_admin_threads(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    last_read_message_id UUID REFERENCES group_admin_messages(id) ON DELETE SET NULL,
    last_read_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (thread_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_group_admin_thread_reads_user_id
    ON group_admin_thread_reads(user_id);

CREATE TABLE IF NOT EXISTS group_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    reporter_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL,
    target_id UUID,
    reason TEXT NOT NULL,
    details TEXT,
    status TEXT NOT NULL DEFAULT 'open',
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT group_reports_target_type_chk CHECK (target_type IN ('group', 'member', 'post', 'comment')),
    CONSTRAINT group_reports_status_chk CHECK (status IN ('open', 'reviewing', 'resolved', 'dismissed'))
);

CREATE INDEX IF NOT EXISTS idx_group_reports_group_status_created
    ON group_reports(group_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS group_audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    target_type TEXT,
    target_id UUID,
    metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_group_audit_events_group_created
    ON group_audit_events(group_id, created_at DESC);

CREATE TABLE IF NOT EXISTS group_notification_preferences (
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    post_notifications BOOLEAN NOT NULL DEFAULT TRUE,
    comment_notifications BOOLEAN NOT NULL DEFAULT TRUE,
    admin_notifications BOOLEAN NOT NULL DEFAULT TRUE,
    muted_until TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, user_id)
);

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
    support_request_id UUID,
    CONSTRAINT group_posts_post_type_chk CHECK (post_type IN ('standard', 'milestone', 'need_support', 'admin_announcement', 'check_in')),
    CONSTRAINT group_posts_body_len_chk CHECK (char_length(body) BETWEEN 1 AND 4000)
);

CREATE INDEX IF NOT EXISTS idx_group_posts_group_pinned_created
    ON group_posts(group_id, pinned_at DESC NULLS LAST, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_group_posts_user_created
    ON group_posts(user_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS group_posts_support_request_id_unique_idx
    ON group_posts(support_request_id)
    WHERE support_request_id IS NOT NULL;

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

CREATE TABLE IF NOT EXISTS post_reactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type TEXT,
    UNIQUE (post_id, user_id, type)
);

CREATE INDEX IF NOT EXISTS idx_post_reactions_post_id_type
    ON post_reactions(post_id, type);

CREATE TABLE IF NOT EXISTS comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_comments_post_id_created_at
    ON comments(post_id, created_at);

CREATE INDEX IF NOT EXISTS idx_comments_user_id
    ON comments(user_id);

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_onboarding_owner_welcome_comment_id_fkey;

ALTER TABLE users
    ADD CONSTRAINT users_onboarding_owner_welcome_comment_id_fkey
        FOREIGN KEY (onboarding_owner_welcome_comment_id) REFERENCES comments(id) ON DELETE SET NULL;

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

CREATE INDEX IF NOT EXISTS idx_feed_muted_authors_user_muted_author
    ON feed_muted_authors(user_id, muted_at DESC, author_id DESC);

CREATE TABLE IF NOT EXISTS feed_impressions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id UUID NOT NULL,
    item_kind TEXT NOT NULL CHECK (item_kind IN ('post', 'reshare')),
    feed_mode TEXT NOT NULL CHECK (feed_mode IN ('home')),
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

CREATE UNIQUE INDEX IF NOT EXISTS idx_feed_impressions_session_item_served
    ON feed_impressions(user_id, item_id, item_kind, feed_mode, session_id, served_at);

CREATE TABLE IF NOT EXISTS feed_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id UUID NOT NULL,
    item_kind TEXT NOT NULL CHECK (item_kind IN ('post', 'reshare')),
    feed_mode TEXT NOT NULL CHECK (feed_mode IN ('home')),
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

CREATE TABLE IF NOT EXISTS post_quality_features (
    post_id UUID PRIMARY KEY REFERENCES posts(id) ON DELETE CASCADE,
    author_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    has_body BOOLEAN NOT NULL DEFAULT FALSE,
    has_image BOOLEAN NOT NULL DEFAULT FALSE,
    body_length INT NOT NULL DEFAULT 0,
    total_impression_count INT NOT NULL DEFAULT 0,
    total_like_count INT NOT NULL DEFAULT 0,
    total_comment_count INT NOT NULL DEFAULT 0,
    total_share_count INT NOT NULL DEFAULT 0,
    total_hide_count INT NOT NULL DEFAULT 0,
    recent_impression_count INT NOT NULL DEFAULT 0,
    recent_like_count INT NOT NULL DEFAULT 0,
    recent_comment_count INT NOT NULL DEFAULT 0,
    recent_share_count INT NOT NULL DEFAULT 0,
    recent_hide_count INT NOT NULL DEFAULT 0,
    quality_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_engagement_at TIMESTAMPTZ,
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

CREATE TABLE IF NOT EXISTS feed_aggregate_jobs (
    target_kind TEXT NOT NULL CHECK (target_kind IN ('post', 'share', 'author')),
    target_id UUID NOT NULL,
    queued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at TIMESTAMPTZ,
    attempt_count INT NOT NULL DEFAULT 0,
    last_error TEXT,
    PRIMARY KEY (target_kind, target_id)
);

CREATE INDEX IF NOT EXISTS idx_feed_aggregate_jobs_available
    ON feed_aggregate_jobs(available_at ASC, queued_at ASC)
    WHERE claimed_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_feed_aggregate_jobs_claimed
    ON feed_aggregate_jobs(claimed_at ASC)
    WHERE claimed_at IS NOT NULL;

CREATE OR REPLACE FUNCTION enqueue_feed_aggregate_job(target_kind_in TEXT, target_id_in UUID)
RETURNS VOID AS $$
BEGIN
    IF target_id_in IS NULL OR target_kind_in IS NULL THEN
        RETURN;
    END IF;

    INSERT INTO feed_aggregate_jobs (
        target_kind,
        target_id,
        queued_at,
        available_at,
        claimed_at,
        last_error
    ) VALUES (
        target_kind_in,
        target_id_in,
        NOW(),
        NOW(),
        NULL,
        NULL
    )
    ON CONFLICT (target_kind, target_id) DO UPDATE
    SET queued_at = EXCLUDED.queued_at,
        available_at = EXCLUDED.available_at,
        last_error = NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION trigger_enqueue_feed_aggregate_job()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_TABLE_NAME = 'posts' THEN
        IF TG_OP = 'INSERT' THEN
            PERFORM enqueue_feed_aggregate_job('post', NEW.id);
            RETURN NEW;
        END IF;

        PERFORM enqueue_feed_aggregate_job('author', OLD.user_id);
        RETURN OLD;
    END IF;

    IF TG_TABLE_NAME = 'post_shares' THEN
        IF TG_OP = 'INSERT' THEN
            PERFORM enqueue_feed_aggregate_job('share', NEW.id);
            RETURN NEW;
        END IF;

        PERFORM enqueue_feed_aggregate_job('author', OLD.user_id);
        RETURN OLD;
    END IF;

    IF TG_TABLE_NAME = 'feed_events' THEN
        IF NEW.event_type <> 'like' THEN
            RETURN NEW;
        END IF;

        IF NEW.item_kind = 'post' THEN
            PERFORM enqueue_feed_aggregate_job('post', NEW.item_id);
        ELSIF NEW.item_kind = 'reshare' THEN
            PERFORM enqueue_feed_aggregate_job('share', NEW.item_id);
        END IF;
        RETURN NEW;
    END IF;

    IF TG_TABLE_NAME = 'feed_hidden_posts' THEN
        IF TG_OP = 'DELETE' THEN
            IF OLD.item_kind = 'post' THEN
                PERFORM enqueue_feed_aggregate_job('post', OLD.item_id);
            ELSIF OLD.item_kind = 'reshare' THEN
                PERFORM enqueue_feed_aggregate_job('share', OLD.item_id);
            END IF;
            RETURN OLD;
        END IF;

        IF NEW.item_kind = 'post' THEN
            PERFORM enqueue_feed_aggregate_job('post', NEW.item_id);
        ELSIF NEW.item_kind = 'reshare' THEN
            PERFORM enqueue_feed_aggregate_job('share', NEW.item_id);
        END IF;
        RETURN NEW;
    END IF;

    IF TG_TABLE_NAME = 'post_reactions' OR TG_TABLE_NAME = 'comments' THEN
        IF TG_OP = 'DELETE' THEN
            PERFORM enqueue_feed_aggregate_job('post', OLD.post_id);
            RETURN OLD;
        END IF;

        PERFORM enqueue_feed_aggregate_job('post', NEW.post_id);
        RETURN NEW;
    END IF;

    IF TG_TABLE_NAME = 'share_reactions' OR TG_TABLE_NAME = 'share_comments' THEN
        IF TG_OP = 'DELETE' THEN
            PERFORM enqueue_feed_aggregate_job('share', OLD.share_id);
            RETURN OLD;
        END IF;

        PERFORM enqueue_feed_aggregate_job('share', NEW.share_id);
        RETURN NEW;
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_posts_enqueue_feed_aggregate_job ON posts;
CREATE TRIGGER trg_posts_enqueue_feed_aggregate_job
AFTER INSERT OR DELETE ON posts
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

DROP TRIGGER IF EXISTS trg_post_shares_enqueue_feed_aggregate_job ON post_shares;
CREATE TRIGGER trg_post_shares_enqueue_feed_aggregate_job
AFTER INSERT OR DELETE ON post_shares
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

DROP TRIGGER IF EXISTS trg_feed_events_enqueue_feed_aggregate_job ON feed_events;
CREATE TRIGGER trg_feed_events_enqueue_feed_aggregate_job
AFTER INSERT ON feed_events
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

DROP TRIGGER IF EXISTS trg_feed_hidden_posts_enqueue_feed_aggregate_job ON feed_hidden_posts;
CREATE TRIGGER trg_feed_hidden_posts_enqueue_feed_aggregate_job
AFTER INSERT OR UPDATE OR DELETE ON feed_hidden_posts
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

DROP TRIGGER IF EXISTS trg_post_reactions_enqueue_feed_aggregate_job ON post_reactions;
CREATE TRIGGER trg_post_reactions_enqueue_feed_aggregate_job
AFTER INSERT OR DELETE ON post_reactions
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

DROP TRIGGER IF EXISTS trg_comments_enqueue_feed_aggregate_job ON comments;
CREATE TRIGGER trg_comments_enqueue_feed_aggregate_job
AFTER INSERT ON comments
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

DROP TRIGGER IF EXISTS trg_share_reactions_enqueue_feed_aggregate_job ON share_reactions;
CREATE TRIGGER trg_share_reactions_enqueue_feed_aggregate_job
AFTER INSERT OR DELETE ON share_reactions
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

DROP TRIGGER IF EXISTS trg_share_comments_enqueue_feed_aggregate_job ON share_comments;
CREATE TRIGGER trg_share_comments_enqueue_feed_aggregate_job
AFTER INSERT ON share_comments
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

CREATE TABLE IF NOT EXISTS event_categories (
    slug TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    sort_order INT NOT NULL DEFAULT 0
);

INSERT INTO event_categories (slug, label, sort_order) VALUES
    ('recovery', 'Recovery', 10),
    ('social', 'Social', 20),
    ('activity', 'Activities', 30),
    ('wellness', 'Wellness', 40),
    ('online', 'Online', 50),
    ('service', 'Service', 60)
ON CONFLICT (slug) DO UPDATE SET
    label = EXCLUDED.label,
    sort_order = EXCLUDED.sort_order;

CREATE TABLE IF NOT EXISTS meetups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organiser_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT,
    category_slug TEXT REFERENCES event_categories(slug) ON DELETE SET NULL,
    event_type TEXT NOT NULL DEFAULT 'in_person',
    status TEXT NOT NULL DEFAULT 'published',
    visibility TEXT NOT NULL DEFAULT 'public',
    city TEXT,
    country TEXT,
    venue_name TEXT,
    address_line_1 TEXT,
    address_line_2 TEXT,
    how_to_find_us TEXT,
    online_url TEXT,
    cover_image_url TEXT,
    starts_at TIMESTAMPTZ,
    ends_at TIMESTAMPTZ,
    timezone TEXT NOT NULL DEFAULT 'UTC',
    lat DOUBLE PRECISION,
    lng DOUBLE PRECISION,
    capacity INT,
    attendee_count INT NOT NULL DEFAULT 0,
    waitlist_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    waitlist_count INT NOT NULL DEFAULT 0,
    saved_count INT NOT NULL DEFAULT 0,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT meetups_status_chk CHECK (status IN ('published', 'cancelled', 'completed'))
);

CREATE INDEX IF NOT EXISTS idx_meetups_city
    ON meetups(city);

CREATE INDEX IF NOT EXISTS idx_meetups_starts_at
    ON meetups(starts_at);

CREATE INDEX IF NOT EXISTS idx_meetups_status_starts_at
    ON meetups(status, starts_at);

CREATE INDEX IF NOT EXISTS idx_meetups_status_visibility_starts_at
    ON meetups(status, visibility, starts_at);

CREATE INDEX IF NOT EXISTS idx_meetups_category_starts_at
    ON meetups(category_slug, starts_at);

CREATE INDEX IF NOT EXISTS idx_meetups_event_type_starts_at
    ON meetups(event_type, starts_at);

CREATE INDEX IF NOT EXISTS idx_meetups_lat_lng
    ON meetups(lat, lng);

CREATE TABLE IF NOT EXISTS meetup_attendees (
    meetup_id UUID NOT NULL REFERENCES meetups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rsvp_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (meetup_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_meetup_attendees_meetup_id
    ON meetup_attendees(meetup_id);

CREATE INDEX IF NOT EXISTS idx_meetup_attendees_user_meetup_id
    ON meetup_attendees(user_id, meetup_id);

CREATE TABLE IF NOT EXISTS meetup_hosts (
    meetup_id UUID NOT NULL REFERENCES meetups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'co_host',
    PRIMARY KEY (meetup_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_meetup_hosts_meetup_id
    ON meetup_hosts(meetup_id);

CREATE TABLE IF NOT EXISTS meetup_waitlist (
    meetup_id UUID NOT NULL REFERENCES meetups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (meetup_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_meetup_waitlist_meetup_id
    ON meetup_waitlist(meetup_id);

CREATE TABLE IF NOT EXISTS support_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requester_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN (
        'need_to_talk',
        'need_distraction',
        'need_encouragement',
        'need_in_person_help'
    )),
    message TEXT,
    city TEXT,
    channel TEXT NOT NULL DEFAULT 'community' CHECK (channel IN ('immediate', 'community')),
    urgency TEXT NOT NULL DEFAULT 'when_you_can' CHECK (urgency IN ('when_you_can', 'soon', 'right_now')),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'active', 'closed')),
    privacy_level TEXT NOT NULL DEFAULT 'standard' CHECK (privacy_level IN ('standard', 'private')),
    accepted_response_id UUID,
    accepted_responder_id UUID REFERENCES users(id) ON DELETE SET NULL,
    accepted_at TIMESTAMPTZ,
    chat_id UUID,
    response_count INT NOT NULL DEFAULT 0,
    last_response_at TIMESTAMPTZ,
    group_post_id UUID REFERENCES group_posts(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_support_requests_requester_created_at
    ON support_requests(requester_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_requests_channel_status_created_at
    ON support_requests(channel, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_requests_open_queue
    ON support_requests(channel, created_at, id)
    WHERE status = 'open';

CREATE INDEX IF NOT EXISTS idx_support_requests_requester_status_created_at
    ON support_requests(requester_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_requests_accepted_responder_status_created_at
    ON support_requests(accepted_responder_id, status, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS support_requests_group_post_id_unique_idx
    ON support_requests(group_post_id)
    WHERE group_post_id IS NOT NULL;

ALTER TABLE group_posts
    DROP CONSTRAINT IF EXISTS group_posts_support_request_id_fkey;

ALTER TABLE group_posts
    ADD CONSTRAINT group_posts_support_request_id_fkey
        FOREIGN KEY (support_request_id) REFERENCES support_requests(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS chats (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    is_group BOOLEAN NOT NULL DEFAULT FALSE,
    name TEXT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'closed')),
    support_request_id UUID REFERENCES support_requests(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    next_message_seq BIGINT NOT NULL DEFAULT 1,
    last_message_id UUID,
    last_message_sender_id UUID REFERENCES users(id) ON DELETE SET NULL,
    last_message_body TEXT,
    last_message_at TIMESTAMPTZ,
    last_message_seq BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_chats_support_request_id
    ON chats(support_request_id);

CREATE INDEX IF NOT EXISTS idx_chats_last_message_at
    ON chats(last_message_at DESC);

CREATE INDEX IF NOT EXISTS idx_chats_last_message_sender_id
    ON chats(last_message_sender_id);

CREATE TABLE IF NOT EXISTS chat_members (
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('requester', 'addressee')),
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chat_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_chat_members_user_id
    ON chat_members(user_id);

CREATE TABLE IF NOT EXISTS messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    sender_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'user' CHECK (kind IN ('user', 'system')),
    body TEXT,
    client_message_id TEXT,
    chat_seq BIGINT,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_messages_chat_id
    ON messages(chat_id);

CREATE INDEX IF NOT EXISTS idx_messages_sent_at
    ON messages(sent_at);

CREATE INDEX IF NOT EXISTS idx_messages_chat_id_sent_at
    ON messages(chat_id, sent_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_chat_id_client_message_id
    ON messages(chat_id, client_message_id)
    WHERE client_message_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_chat_id_chat_seq
    ON messages(chat_id, chat_seq)
    WHERE chat_seq IS NOT NULL;

ALTER TABLE chats
    DROP CONSTRAINT IF EXISTS chats_last_message_id_fkey;

ALTER TABLE chats
    ADD CONSTRAINT chats_last_message_id_fkey
        FOREIGN KEY (last_message_id) REFERENCES messages(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS support_responses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    support_request_id UUID NOT NULL REFERENCES support_requests(id) ON DELETE CASCADE,
    responder_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    response_type TEXT NOT NULL CHECK (response_type IN ('can_chat', 'check_in_later', 'can_meet')),
    message TEXT,
    scheduled_for TIMESTAMPTZ,
    chat_id UUID REFERENCES chats(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'not_selected')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (support_request_id, responder_id, response_type)
);

CREATE INDEX IF NOT EXISTS idx_support_responses_request_created_at
    ON support_responses(support_request_id, created_at);

CREATE INDEX IF NOT EXISTS idx_support_responses_responder_request
    ON support_responses(responder_id, support_request_id);

CREATE INDEX IF NOT EXISTS idx_support_responses_chat_id
    ON support_responses(chat_id);

ALTER TABLE support_requests
    DROP CONSTRAINT IF EXISTS support_requests_accepted_response_id_fkey;

ALTER TABLE support_requests
    DROP CONSTRAINT IF EXISTS support_requests_chat_id_fkey;

ALTER TABLE support_requests
    ADD CONSTRAINT support_requests_accepted_response_id_fkey
        FOREIGN KEY (accepted_response_id) REFERENCES support_responses(id) ON DELETE SET NULL;

ALTER TABLE support_requests
    ADD CONSTRAINT support_requests_chat_id_fkey
        FOREIGN KEY (chat_id) REFERENCES chats(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS friendships (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_a_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_b_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    requester_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('pending', 'accepted')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    accepted_at TIMESTAMPTZ,
    CHECK (user_a_id <> user_b_id),
    CHECK (requester_id = user_a_id OR requester_id = user_b_id),
    UNIQUE (user_a_id, user_b_id)
);

CREATE INDEX IF NOT EXISTS friendships_user_a_id_idx
    ON friendships(user_a_id);

CREATE INDEX IF NOT EXISTS friendships_user_b_id_idx
    ON friendships(user_b_id);

CREATE INDEX IF NOT EXISTS friendships_requester_id_idx
    ON friendships(requester_id);

CREATE INDEX IF NOT EXISTS friendships_status_idx
    ON friendships(status);

CREATE INDEX IF NOT EXISTS idx_friendships_status_user_a
    ON friendships(status, user_a_id);

CREATE INDEX IF NOT EXISTS idx_friendships_status_user_b
    ON friendships(status, user_b_id);

CREATE TABLE IF NOT EXISTS user_blocks (
    blocker_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (blocker_id, blocked_id),
    CHECK (blocker_id <> blocked_id)
);

CREATE INDEX IF NOT EXISTS idx_user_blocks_blocked_id
    ON user_blocks(blocked_id);

CREATE INDEX IF NOT EXISTS idx_user_blocks_blocker_created
    ON user_blocks(blocker_id, created_at DESC, blocked_id DESC);

CREATE TABLE IF NOT EXISTS user_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reported_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason TEXT NOT NULL,
    details TEXT,
    status TEXT NOT NULL DEFAULT 'open',
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (reporter_id <> reported_user_id),
    CONSTRAINT user_reports_reason_chk CHECK (reason IN ('unwanted_advances', 'harassment', 'spam', 'safety_concern', 'other')),
    CONSTRAINT user_reports_status_chk CHECK (status IN ('open', 'reviewing', 'resolved', 'dismissed'))
);

CREATE INDEX IF NOT EXISTS idx_user_reports_reported_status_created
    ON user_reports(reported_user_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_user_reports_reporter_created
    ON user_reports(reporter_id, created_at DESC);

CREATE TABLE IF NOT EXISTS content_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL,
    target_id UUID NOT NULL,
    reason TEXT NOT NULL,
    details TEXT,
    context JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'open',
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT content_reports_target_type_chk CHECK (
        target_type IN (
            'feed_post',
            'feed_share',
            'feed_comment',
            'feed_share_comment',
            'chat',
            'message'
        )
    ),
    CONSTRAINT content_reports_reason_chk CHECK (
        reason IN (
            'harassment',
            'spam',
            'safety_concern',
            'hate',
            'sexual_content',
            'violence',
            'self_harm',
            'other'
        )
    ),
    CONSTRAINT content_reports_status_chk CHECK (status IN ('open', 'reviewing', 'resolved', 'dismissed'))
);

CREATE INDEX IF NOT EXISTS idx_content_reports_target_created
    ON content_reports(target_type, target_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_content_reports_status_created
    ON content_reports(status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_content_reports_reporter_created
    ON content_reports(reporter_id, created_at DESC);

CREATE TABLE IF NOT EXISTS content_moderation_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    surface TEXT NOT NULL,
    content_kind TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    action TEXT NOT NULL,
    flagged BOOLEAN NOT NULL DEFAULT FALSE,
    categories JSONB NOT NULL DEFAULT '{}'::jsonb,
    category_scores JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT content_moderation_events_action_chk CHECK (action IN ('allowed', 'blocked', 'provider_error'))
);

CREATE INDEX IF NOT EXISTS idx_content_moderation_events_surface_created
    ON content_moderation_events(surface, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_content_moderation_events_action_created
    ON content_moderation_events(action, created_at DESC);

CREATE TABLE IF NOT EXISTS discover_impressions (
    viewer_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    candidate_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source TEXT NOT NULL,
    shown_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (viewer_id, candidate_id, shown_at)
);

CREATE INDEX IF NOT EXISTS idx_discover_impressions_viewer_shown_at
    ON discover_impressions(viewer_id, shown_at DESC);

CREATE INDEX IF NOT EXISTS idx_discover_impressions_viewer_candidate
    ON discover_impressions(viewer_id, candidate_id, shown_at DESC);

CREATE TABLE IF NOT EXISTS dating_impressions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    viewer_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    candidate_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source TEXT NOT NULL,
    rank_score DOUBLE PRECISION,
    rank_position INT,
    request_id UUID,
    shown_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (viewer_id <> candidate_id)
);

CREATE INDEX IF NOT EXISTS idx_dating_impressions_viewer_shown_at
    ON dating_impressions(viewer_id, shown_at DESC);

CREATE INDEX IF NOT EXISTS idx_dating_impressions_viewer_candidate
    ON dating_impressions(viewer_id, candidate_id, shown_at DESC);

CREATE INDEX IF NOT EXISTS idx_dating_impressions_candidate_shown_at
    ON dating_impressions(candidate_id, shown_at DESC);

CREATE TABLE IF NOT EXISTS dating_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (actor_id <> target_id),
    CONSTRAINT dating_actions_action_chk CHECK (action IN ('like', 'pass')),
    UNIQUE (actor_id, target_id)
);

CREATE INDEX IF NOT EXISTS idx_dating_actions_actor_updated
    ON dating_actions(actor_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_dating_actions_target_like
    ON dating_actions(target_id, actor_id)
    WHERE action = 'like';

CREATE INDEX IF NOT EXISTS idx_dating_actions_target_like_updated
    ON dating_actions(target_id, action, updated_at DESC, id DESC)
    WHERE action = 'like';

CREATE TABLE IF NOT EXISTS dating_matches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_a_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_b_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    chat_id UUID REFERENCES chats(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'active',
    matched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    unmatched_at TIMESTAMPTZ,
    unmatched_by UUID REFERENCES users(id) ON DELETE SET NULL,
    CHECK (user_a_id < user_b_id),
    CONSTRAINT dating_matches_status_chk CHECK (status IN ('active', 'unmatched')),
    CONSTRAINT dating_matches_unmatch_chk CHECK (
        (status = 'active' AND unmatched_at IS NULL AND unmatched_by IS NULL)
        OR (status = 'unmatched' AND unmatched_at IS NOT NULL AND unmatched_by IS NOT NULL)
    ),
    UNIQUE (user_a_id, user_b_id)
);

CREATE INDEX IF NOT EXISTS idx_dating_matches_user_a_status
    ON dating_matches(user_a_id, status, matched_at DESC);

CREATE INDEX IF NOT EXISTS idx_dating_matches_user_b_status
    ON dating_matches(user_b_id, status, matched_at DESC);

CREATE INDEX IF NOT EXISTS idx_dating_matches_chat_id
    ON dating_matches(chat_id);

CREATE INDEX IF NOT EXISTS idx_users_dating_geo_active
    ON users(discover_lat, discover_lng, last_active_at DESC)
    WHERE connection_intents @> ARRAY['dating']::text[];

CREATE INDEX IF NOT EXISTS idx_users_dating_last_active
    ON users(last_active_at DESC, profile_completeness DESC, id DESC)
    WHERE connection_intents @> ARRAY['dating']::text[];

CREATE INDEX IF NOT EXISTS idx_users_dating_created_at
    ON users(created_at DESC, id DESC)
    WHERE connection_intents @> ARRAY['dating']::text[];

CREATE TABLE IF NOT EXISTS comment_mentions (
    comment_id UUID NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (comment_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_comment_mentions_user_id
    ON comment_mentions(user_id);

CREATE TABLE IF NOT EXISTS user_devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    push_token TEXT NOT NULL UNIQUE,
    platform TEXT NOT NULL CHECK (platform IN ('ios', 'android')),
    device_name TEXT,
    app_version TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    disabled_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_user_devices_user_id
    ON user_devices(user_id);

CREATE INDEX IF NOT EXISTS idx_user_devices_active_user_id
    ON user_devices(user_id)
    WHERE disabled_at IS NULL;

CREATE TABLE IF NOT EXISTS notification_preferences (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    chat_messages BOOLEAN NOT NULL DEFAULT TRUE,
    comment_mentions BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    resource_type TEXT NOT NULL,
    resource_id UUID,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    read_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_notifications_user_id_created_at
    ON notifications(user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_notifications_user_id_unread
    ON notifications(user_id, created_at DESC)
    WHERE read_at IS NULL;

CREATE TABLE IF NOT EXISTS notification_counters (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    unread_count INTEGER NOT NULL DEFAULT 0 CHECK (unread_count >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS notification_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_id UUID NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    user_device_id UUID REFERENCES user_devices(id) ON DELETE SET NULL,
    provider TEXT NOT NULL,
    push_token TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'sent', 'failed', 'cancelled')) DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    provider_message_id TEXT,
    last_error TEXT,
    scheduled_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_pending
    ON notification_deliveries(status, scheduled_at)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_notification_id
    ON notification_deliveries(notification_id);

CREATE TABLE IF NOT EXISTS chat_reads (
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    last_read_message_id UUID REFERENCES messages(id) ON DELETE SET NULL,
    last_read_chat_seq BIGINT NOT NULL DEFAULT 0,
    last_read_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chat_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_chat_reads_user_id
    ON chat_reads(user_id);

CREATE TABLE IF NOT EXISTS recovery_meeting_import_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    snapshot_path TEXT NOT NULL,
    snapshot_sha256 TEXT NOT NULL,
    snapshot_schema_version TEXT NOT NULL,
    snapshot_generated_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    meetings_seen INTEGER NOT NULL DEFAULT 0 CHECK (meetings_seen >= 0),
    meetings_upserted INTEGER NOT NULL DEFAULT 0 CHECK (meetings_upserted >= 0),
    occurrences_written INTEGER NOT NULL DEFAULT 0 CHECK (occurrences_written >= 0),
    stale_marked INTEGER NOT NULL DEFAULT 0 CHECK (stale_marked >= 0),
    inactive_marked INTEGER NOT NULL DEFAULT 0 CHECK (inactive_marked >= 0),
    error_message TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_recovery_meeting_import_runs_sha256
    ON recovery_meeting_import_runs(snapshot_sha256);

CREATE INDEX IF NOT EXISTS idx_recovery_meeting_import_runs_started_at
    ON recovery_meeting_import_runs(started_at DESC);

CREATE TABLE IF NOT EXISTS recovery_meetings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fellowship TEXT NOT NULL,
    source_id TEXT NOT NULL,
    source_record_id TEXT NOT NULL,
    source_url TEXT NOT NULL,
    name TEXT NOT NULL,
    meeting_type TEXT NOT NULL CHECK (meeting_type IN ('in_person', 'online', 'hybrid', 'phone', 'unknown')),
    venue_name TEXT,
    address_line1 TEXT,
    address_line2 TEXT,
    city TEXT,
    region TEXT,
    region_code TEXT,
    postal_code TEXT,
    country TEXT,
    country_code TEXT,
    latitude DOUBLE PRECISION,
    longitude DOUBLE PRECISION,
    is_approximate_location BOOLEAN NOT NULL DEFAULT FALSE,
    online_url TEXT,
    phone_join_info TEXT,
    formats TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    language TEXT,
    accessibility_notes TEXT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'stale', 'inactive')),
    missing_run_count INTEGER NOT NULL DEFAULT 0 CHECK (missing_run_count >= 0),
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_verified_at TIMESTAMPTZ,
    last_import_run_id UUID REFERENCES recovery_meeting_import_runs(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (fellowship, source_id, source_record_id)
);

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_status
    ON recovery_meetings(status);

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_fellowship
    ON recovery_meetings(fellowship);

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_country_city
    ON recovery_meetings(country, city);

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_country_region_city
    ON recovery_meetings(country, region, city)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_country_code_region_code_city
    ON recovery_meetings(country_code, region_code, city)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_region_code
    ON recovery_meetings(region_code)
    WHERE status = 'active' AND region_code IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_meeting_type
    ON recovery_meetings(meeting_type);

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_last_import_run_id
    ON recovery_meetings(last_import_run_id);

CREATE TABLE IF NOT EXISTS recovery_meeting_occurrences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recovery_meeting_id UUID NOT NULL REFERENCES recovery_meetings(id) ON DELETE CASCADE,
    day_of_week SMALLINT NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
    start_time_local TIME NOT NULL,
    end_time_local TIME,
    timezone TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_recovery_meeting_occurrences_unique
    ON recovery_meeting_occurrences(
        recovery_meeting_id,
        day_of_week,
        start_time_local,
        COALESCE(end_time_local, TIME '00:00'),
        timezone
    );

CREATE INDEX IF NOT EXISTS idx_recovery_meeting_occurrences_day_time
    ON recovery_meeting_occurrences(day_of_week, start_time_local);
