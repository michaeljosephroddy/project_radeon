CREATE INDEX IF NOT EXISTS idx_users_gender
    ON users(gender);

CREATE INDEX IF NOT EXISTS idx_users_sobriety_band
    ON users(sobriety_band);

CREATE INDEX IF NOT EXISTS idx_users_last_active_at_desc
    ON users(last_active_at DESC);

CREATE INDEX IF NOT EXISTS idx_users_discover_lat_lng
    ON users(discover_lat, discover_lng);

CREATE INDEX IF NOT EXISTS idx_friendships_status_user_a
    ON friendships(status, user_a_id);

CREATE INDEX IF NOT EXISTS idx_friendships_status_user_b
    ON friendships(status, user_b_id);

CREATE INDEX IF NOT EXISTS idx_discover_impressions_viewer_shown_at
    ON discover_impressions(viewer_id, shown_at DESC);

CREATE INDEX IF NOT EXISTS idx_discover_impressions_viewer_candidate
    ON discover_impressions(viewer_id, candidate_id, shown_at DESC);

CREATE INDEX IF NOT EXISTS idx_discover_dismissals_viewer_dismissed_at
    ON discover_dismissals(viewer_id, dismissed_at DESC);
