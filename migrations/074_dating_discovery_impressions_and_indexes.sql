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

CREATE INDEX IF NOT EXISTS idx_users_dating_geo_active
    ON users(discover_lat, discover_lng, last_active_at DESC)
    WHERE connection_intents @> ARRAY['dating']::text[];

CREATE INDEX IF NOT EXISTS idx_users_dating_last_active
    ON users(last_active_at DESC, profile_completeness DESC, id DESC)
    WHERE connection_intents @> ARRAY['dating']::text[];

CREATE INDEX IF NOT EXISTS idx_users_dating_created_at
    ON users(created_at DESC, id DESC)
    WHERE connection_intents @> ARRAY['dating']::text[];
