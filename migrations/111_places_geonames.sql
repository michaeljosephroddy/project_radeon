CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS places (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source TEXT NOT NULL,
    source_id TEXT NOT NULL,
    name TEXT NOT NULL,
    ascii_name TEXT,
    alternate_names TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    country_code TEXT NOT NULL,
    country_name TEXT,
    admin1_code TEXT,
    admin1_name TEXT,
    admin2_code TEXT,
    admin2_name TEXT,
    feature_class TEXT,
    feature_code TEXT,
    population INTEGER NOT NULL DEFAULT 0 CHECK (population >= 0),
    latitude DOUBLE PRECISION NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude DOUBLE PRECISION NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    timezone TEXT,
    search_text TEXT NOT NULL,
    name_normalized TEXT GENERATED ALWAYS AS (LOWER(name)) STORED,
    ascii_name_normalized TEXT GENERATED ALWAYS AS (LOWER(COALESCE(ascii_name, ''))) STORED,
    search_text_normalized TEXT GENERATED ALWAYS AS (LOWER(search_text)) STORED,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source, source_id)
);

CREATE INDEX IF NOT EXISTS idx_places_country_code
    ON places(country_code);

CREATE INDEX IF NOT EXISTS idx_places_country_admin1
    ON places(country_code, admin1_code);

CREATE INDEX IF NOT EXISTS idx_places_lat_lng
    ON places(latitude, longitude);

CREATE INDEX IF NOT EXISTS idx_places_name_normalized_pattern
    ON places(name_normalized text_pattern_ops);

CREATE INDEX IF NOT EXISTS idx_places_ascii_name_normalized_pattern
    ON places(ascii_name_normalized text_pattern_ops);

CREATE INDEX IF NOT EXISTS idx_places_search_text_trgm
    ON places USING GIN(search_text_normalized gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_places_name_trgm
    ON places USING GIN(name_normalized gin_trgm_ops);

CREATE TABLE IF NOT EXISTS recovery_meeting_place_matches (
    recovery_meeting_id UUID PRIMARY KEY REFERENCES recovery_meetings(id) ON DELETE CASCADE,
    place_id UUID NOT NULL REFERENCES places(id) ON DELETE CASCADE,
    match_level TEXT NOT NULL CHECK (match_level IN (
        'city_country',
        'city_region_country',
        'postal_code',
        'coordinate_nearest',
        'manual'
    )),
    confidence INTEGER NOT NULL CHECK (confidence BETWEEN 0 AND 100),
    matched_text TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_recovery_meeting_place_matches_place
    ON recovery_meeting_place_matches(place_id);

CREATE INDEX IF NOT EXISTS idx_recovery_meeting_place_matches_place_confidence
    ON recovery_meeting_place_matches(place_id, confidence DESC);
