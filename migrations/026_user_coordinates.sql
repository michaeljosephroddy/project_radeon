ALTER TABLE users
    ADD COLUMN IF NOT EXISTS lat double precision,
    ADD COLUMN IF NOT EXISTS lng double precision;
