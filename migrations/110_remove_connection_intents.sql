DROP INDEX IF EXISTS idx_users_connection_intents;
DROP INDEX IF EXISTS idx_users_dating_geo_active;
DROP INDEX IF EXISTS idx_users_dating_last_active;
DROP INDEX IF EXISTS idx_users_dating_created_at;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_connection_intents_chk,
    DROP COLUMN IF EXISTS connection_intents;

CREATE INDEX IF NOT EXISTS idx_users_dating_geo_active
    ON users(discover_lat, discover_lng, last_active_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_users_dating_last_active
    ON users(last_active_at DESC, profile_completeness DESC, id DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_users_dating_created_at
    ON users(created_at DESC, id DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_dating_profiles_user_completed_paused
    ON dating_profiles(user_id, completed_at, paused);
