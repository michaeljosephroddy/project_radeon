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
