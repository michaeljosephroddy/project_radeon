ALTER TABLE users
    ADD COLUMN IF NOT EXISTS discover_lat            double precision,
    ADD COLUMN IF NOT EXISTS discover_lng            double precision,
    ADD COLUMN IF NOT EXISTS sobriety_band           smallint,
    ADD COLUMN IF NOT EXISTS profile_completeness    smallint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_active_at          timestamptz NOT NULL DEFAULT NOW();

UPDATE users u
SET
    discover_lat = COALESCE(u.current_lat, u.lat),
    discover_lng = COALESCE(u.current_lng, u.lng),
    sobriety_band = CASE
        WHEN u.sober_since IS NULL THEN NULL
        WHEN CURRENT_DATE - u.sober_since < 30 THEN 1
        WHEN CURRENT_DATE - u.sober_since < 90 THEN 2
        WHEN CURRENT_DATE - u.sober_since < 365 THEN 3
        WHEN CURRENT_DATE - u.sober_since < 730 THEN 4
        WHEN CURRENT_DATE - u.sober_since < 1825 THEN 5
        ELSE 6
    END,
    profile_completeness = (
        CASE WHEN NULLIF(u.avatar_url, '') IS NOT NULL THEN 1 ELSE 0 END
        + CASE WHEN NULLIF(u.city, '') IS NOT NULL THEN 1 ELSE 0 END
        + CASE WHEN NULLIF(u.country, '') IS NOT NULL THEN 1 ELSE 0 END
        + CASE WHEN NULLIF(u.bio, '') IS NOT NULL THEN 1 ELSE 0 END
        + CASE WHEN NULLIF(u.gender, '') IS NOT NULL THEN 1 ELSE 0 END
        + CASE WHEN u.birth_date IS NOT NULL THEN 1 ELSE 0 END
        + CASE WHEN u.sober_since IS NOT NULL THEN 1 ELSE 0 END
        + CASE
            WHEN EXISTS (SELECT 1 FROM user_interests ui WHERE ui.user_id = u.id) THEN 1
            ELSE 0
        END
    )::smallint,
    last_active_at = GREATEST(
        u.created_at,
        COALESCE((SELECT MAX(p.created_at) FROM posts p WHERE p.user_id = u.id), u.created_at)
    );
