CREATE TABLE IF NOT EXISTS dating_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bio TEXT,
    relationship_goal TEXT NOT NULL DEFAULT '',
    interested_in_genders TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    age_min INT NOT NULL DEFAULT 18,
    age_max INT NOT NULL DEFAULT 80,
    distance_km INT NOT NULL DEFAULT 50,
    paused BOOLEAN NOT NULL DEFAULT FALSE,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id),
    CONSTRAINT dating_profiles_relationship_goal_chk CHECK (
        relationship_goal IN ('', 'long_term', 'life_partner', 'casual', 'open_to_explore')
    ),
    CONSTRAINT dating_profiles_interested_in_genders_chk CHECK (
        interested_in_genders <@ ARRAY['woman', 'man', 'non_binary']::TEXT[]
    ),
    CONSTRAINT dating_profiles_age_range_chk CHECK (
        age_min BETWEEN 18 AND 100
        AND age_max BETWEEN 18 AND 100
        AND age_min <= age_max
    ),
    CONSTRAINT dating_profiles_distance_chk CHECK (distance_km BETWEEN 0 AND 500)
);

CREATE TABLE IF NOT EXISTS dating_profile_photos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id UUID NOT NULL REFERENCES dating_profiles(id) ON DELETE CASCADE,
    image_url TEXT NOT NULL,
    width INT NOT NULL,
    height INT NOT NULL,
    position INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT dating_profile_photos_dimensions_chk CHECK (width > 0 AND height > 0),
    CONSTRAINT dating_profile_photos_position_chk CHECK (position BETWEEN 0 AND 5),
    UNIQUE (profile_id, position)
);

CREATE INDEX IF NOT EXISTS idx_dating_profiles_completed_active
    ON dating_profiles(completed_at, paused, updated_at DESC)
    WHERE completed_at IS NOT NULL AND paused = FALSE;

CREATE INDEX IF NOT EXISTS idx_dating_profile_photos_profile_position
    ON dating_profile_photos(profile_id, position);

INSERT INTO dating_profiles (
    user_id, bio, age_min, age_max, distance_km, paused, completed_at, created_at, updated_at
)
SELECT
    u.id,
    u.bio,
    18,
    80,
    50,
    FALSE,
    NULL,
    NOW(),
    NOW()
FROM users u
WHERE u.deleted_at IS NULL
    AND u.connection_intents @> ARRAY['dating']::TEXT[]
ON CONFLICT (user_id) DO NOTHING;

INSERT INTO dating_profile_photos (profile_id, image_url, width, height, position)
SELECT dp.id, u.avatar_url, 1, 1, 0
FROM dating_profiles dp
JOIN users u ON u.id = dp.user_id
WHERE NULLIF(u.avatar_url, '') IS NOT NULL
ON CONFLICT (profile_id, position) DO NOTHING;
