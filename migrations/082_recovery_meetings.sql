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
    postal_code TEXT,
    country TEXT,
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
