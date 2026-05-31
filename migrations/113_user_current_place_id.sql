ALTER TABLE users
    ADD COLUMN IF NOT EXISTS current_place_id UUID NULL REFERENCES places(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_users_current_place_id
    ON users(current_place_id)
    WHERE current_place_id IS NOT NULL;

UPDATE users u
SET current_place_id = (
    SELECT p.id
    FROM places p
    WHERE u.current_lat IS NOT NULL
        AND u.current_lng IS NOT NULL
        AND u.current_country IS NOT NULL
        AND LOWER(COALESCE(p.country_name, '')) = LOWER(u.current_country)
        AND p.latitude BETWEEN u.current_lat - 1.5 AND u.current_lat + 1.5
        AND p.longitude BETWEEN u.current_lng - 1.5 AND u.current_lng + 1.5
    ORDER BY
        CASE
            WHEN u.current_city IS NOT NULL
                AND (p.name_normalized = LOWER(u.current_city) OR p.ascii_name_normalized = LOWER(u.current_city))
                THEN 0
            ELSE 1
        END,
        (6371 * 2 * ASIN(SQRT(
            POWER(SIN(RADIANS((p.latitude - u.current_lat) / 2)), 2)
            + COS(RADIANS(u.current_lat)) * COS(RADIANS(p.latitude))
            * POWER(SIN(RADIANS((p.longitude - u.current_lng) / 2)), 2)
        ))) ASC,
        p.population DESC
    LIMIT 1
)
WHERE u.current_place_id IS NULL
    AND u.current_lat IS NOT NULL
    AND u.current_lng IS NOT NULL
    AND u.current_country IS NOT NULL;
