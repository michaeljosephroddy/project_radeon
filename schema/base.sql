CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username TEXT NOT NULL,
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL DEFAULT '',
    avatar_url TEXT,
    banner_url TEXT,
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
    location_updated_at TIMESTAMPTZ,
    discover_lat DOUBLE PRECISION,
    discover_lng DOUBLE PRECISION,
    sobriety_band SMALLINT,
    profile_completeness SMALLINT NOT NULL DEFAULT 0,
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT users_username_format_chk CHECK (username ~ '^[a-z0-9._]{3,20}$'),
    CONSTRAINT users_subscription_tier_chk CHECK (subscription_tier IN ('free', 'plus')),
    CONSTRAINT users_subscription_status_chk CHECK (subscription_status IN ('inactive', 'active', 'canceled', 'expired'))
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

CREATE INDEX IF NOT EXISTS idx_users_sobriety_band
    ON users(sobriety_band);

CREATE INDEX IF NOT EXISTS idx_users_last_active_at_desc
    ON users(last_active_at DESC);

CREATE INDEX IF NOT EXISTS idx_users_discover_lat_lng
    ON users(discover_lat, discover_lng);

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
        claimed_at = NULL,
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

    IF TG_TABLE_NAME = 'feed_impressions' OR TG_TABLE_NAME = 'feed_hidden_posts' THEN
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

DROP TRIGGER IF EXISTS trg_feed_impressions_enqueue_feed_aggregate_job ON feed_impressions;
CREATE TRIGGER trg_feed_impressions_enqueue_feed_aggregate_job
AFTER INSERT OR UPDATE ON feed_impressions
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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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

CREATE TABLE IF NOT EXISTS event_hosts (
    meetup_id UUID NOT NULL REFERENCES meetups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'co_host',
    PRIMARY KEY (meetup_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_event_hosts_meetup_id
    ON event_hosts(meetup_id);

CREATE TABLE IF NOT EXISTS event_waitlist (
    meetup_id UUID NOT NULL REFERENCES meetups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (meetup_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_event_waitlist_meetup_id
    ON event_waitlist(meetup_id);

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

CREATE TABLE IF NOT EXISTS chats (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    is_group BOOLEAN NOT NULL DEFAULT FALSE,
    name TEXT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('request', 'active', 'declined', 'closed')),
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

CREATE TABLE IF NOT EXISTS discover_dismissals (
    viewer_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    candidate_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    dismissed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (viewer_id, candidate_id)
);

CREATE INDEX IF NOT EXISTS idx_discover_dismissals_viewer_dismissed_at
    ON discover_dismissals(viewer_id, dismissed_at DESC);

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
