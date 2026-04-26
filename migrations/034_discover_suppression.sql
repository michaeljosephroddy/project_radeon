CREATE TABLE IF NOT EXISTS discover_impressions (
    viewer_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    candidate_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source text NOT NULL,
    shown_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (viewer_id, candidate_id, shown_at)
);

CREATE TABLE IF NOT EXISTS discover_dismissals (
    viewer_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    candidate_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    dismissed_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (viewer_id, candidate_id)
);
