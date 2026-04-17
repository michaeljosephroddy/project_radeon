ALTER TABLE users
    ADD COLUMN IF NOT EXISTS avatar_url_blurred TEXT;
