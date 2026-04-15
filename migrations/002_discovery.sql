ALTER TABLE users
    ADD COLUMN IF NOT EXISTS lat          double precision,
    ADD COLUMN IF NOT EXISTS lng          double precision,
    ADD COLUMN IF NOT EXISTS interest_vec float8[];

CREATE TABLE IF NOT EXISTS dismissed_users (
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    dismissed_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, dismissed_id)
);

CREATE INDEX IF NOT EXISTS dismissed_users_user_id_idx ON dismissed_users(user_id);
