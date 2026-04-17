-- Remove dating/match feature tables and columns from project_radeon
DROP TABLE IF EXISTS likes CASCADE;
DROP TABLE IF EXISTS dismissed_users CASCADE;
DROP TABLE IF EXISTS user_interests CASCADE;
DROP TABLE IF EXISTS interests CASCADE;
DROP TABLE IF EXISTS connections CASCADE;

ALTER TABLE users
    DROP COLUMN IF EXISTS lat,
    DROP COLUMN IF EXISTS lng,
    DROP COLUMN IF EXISTS interest_vec,
    DROP COLUMN IF EXISTS discovery_radius_km,
    DROP COLUMN IF EXISTS avatar_url_blurred;
