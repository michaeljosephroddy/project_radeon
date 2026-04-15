ALTER TABLE users
    ADD COLUMN IF NOT EXISTS discovery_radius_km integer NOT NULL DEFAULT 50;
