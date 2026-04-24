ALTER TABLE users
    ADD COLUMN IF NOT EXISTS current_lat          double precision,
    ADD COLUMN IF NOT EXISTS current_lng          double precision,
    ADD COLUMN IF NOT EXISTS current_city         text,
    ADD COLUMN IF NOT EXISTS location_updated_at  timestamptz;
