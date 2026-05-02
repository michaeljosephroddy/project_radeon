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
    CONSTRAINT group_admin_threads_status_chk CHECK (status IN ('open', 'resolved'))
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
