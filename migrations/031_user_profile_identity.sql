ALTER TABLE users
    ADD COLUMN IF NOT EXISTS gender text,
    ADD COLUMN IF NOT EXISTS birth_date date;
