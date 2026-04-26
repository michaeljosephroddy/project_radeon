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
    is_available_to_support BOOLEAN NOT NULL DEFAULT FALSE,
    support_modes TEXT[] NOT NULL DEFAULT '{}',
    support_updated_at TIMESTAMPTZ,
    friend_count INT NOT NULL DEFAULT 0,
    lat DOUBLE PRECISION,
    lng DOUBLE PRECISION,
    current_lat DOUBLE PRECISION,
    current_lng DOUBLE PRECISION,
    current_city TEXT,
    location_updated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT users_username_format_chk CHECK (username ~ '^[a-z0-9._]{3,20}$'),
    CONSTRAINT users_subscription_tier_chk CHECK (subscription_tier IN ('free', 'plus')),
    CONSTRAINT users_subscription_status_chk CHECK (subscription_status IN ('inactive', 'active', 'canceled', 'expired'))
);

CREATE UNIQUE INDEX IF NOT EXISTS users_email_unique_idx
    ON users(email);

CREATE UNIQUE INDEX IF NOT EXISTS users_username_unique_idx
    ON users(username);

CREATE INDEX IF NOT EXISTS idx_users_available_to_support
    ON users(id) WHERE is_available_to_support = true;

CREATE INDEX IF NOT EXISTS idx_users_username_trgm
    ON users USING GIN(username gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_users_city
    ON users(city);

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

CREATE TABLE IF NOT EXISTS meetups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organiser_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT,
    city TEXT,
    starts_at TIMESTAMPTZ,
    capacity INT,
    attendee_count INT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_meetups_city
    ON meetups(city);

CREATE INDEX IF NOT EXISTS idx_meetups_starts_at
    ON meetups(starts_at);

CREATE TABLE IF NOT EXISTS meetup_attendees (
    meetup_id UUID NOT NULL REFERENCES meetups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rsvp_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (meetup_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_meetup_attendees_meetup_id
    ON meetup_attendees(meetup_id);

CREATE TABLE IF NOT EXISTS support_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requester_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN (
        'need_to_talk',
        'need_distraction',
        'need_encouragement',
        'need_company'
    )),
    message TEXT,
    audience TEXT NOT NULL CHECK (audience IN ('friends', 'city', 'community')),
    city TEXT,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'matched', 'closed', 'expired')),
    matched_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    response_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_support_requests_requester_created_at
    ON support_requests(requester_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_requests_status_expires_at
    ON support_requests(status, expires_at);

CREATE INDEX IF NOT EXISTS idx_support_requests_city_status_expires_at
    ON support_requests(city, status, expires_at);

CREATE TABLE IF NOT EXISTS chats (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    is_group BOOLEAN NOT NULL DEFAULT FALSE,
    name TEXT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('request', 'active', 'declined')),
    support_request_id UUID REFERENCES support_requests(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_chats_support_request_id
    ON chats(support_request_id);

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
    body TEXT,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_messages_chat_id
    ON messages(chat_id);

CREATE INDEX IF NOT EXISTS idx_messages_sent_at
    ON messages(sent_at);

CREATE INDEX IF NOT EXISTS idx_messages_chat_id_sent_at
    ON messages(chat_id, sent_at DESC);

CREATE TABLE IF NOT EXISTS support_responses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    support_request_id UUID NOT NULL REFERENCES support_requests(id) ON DELETE CASCADE,
    responder_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    response_type TEXT NOT NULL CHECK (response_type IN ('can_chat', 'check_in_later', 'nearby')),
    message TEXT,
    scheduled_for TIMESTAMPTZ,
    chat_id UUID REFERENCES chats(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (support_request_id, responder_id, response_type)
);

CREATE INDEX IF NOT EXISTS idx_support_responses_request_created_at
    ON support_responses(support_request_id, created_at);

CREATE INDEX IF NOT EXISTS idx_support_responses_responder_request
    ON support_responses(responder_id, support_request_id);

CREATE INDEX IF NOT EXISTS idx_support_responses_chat_id
    ON support_responses(chat_id);

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
    last_read_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chat_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_chat_reads_user_id
    ON chat_reads(user_id);
