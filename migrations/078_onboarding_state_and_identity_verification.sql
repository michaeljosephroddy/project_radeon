ALTER TABLE users
    ADD COLUMN IF NOT EXISTS onboarding_completed_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS identity_verification_status TEXT NOT NULL DEFAULT 'not_started',
    ADD COLUMN IF NOT EXISTS identity_verification_provider TEXT NULL,
    ADD COLUMN IF NOT EXISTS identity_verification_session_id TEXT NULL,
    ADD COLUMN IF NOT EXISTS identity_verification_last_error TEXT NULL,
    ADD COLUMN IF NOT EXISTS identity_verified_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS onboarding_first_friend_user_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS onboarding_first_group_id UUID NULL REFERENCES groups(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS onboarding_first_post_id UUID NULL REFERENCES posts(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS onboarding_owner_welcome_comment_id UUID NULL REFERENCES comments(id) ON DELETE SET NULL;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_identity_verification_status_chk;

ALTER TABLE users
    ADD CONSTRAINT users_identity_verification_status_chk
    CHECK (
        identity_verification_status IN (
            'not_started',
            'requires_input',
            'pending',
            'verified',
            'failed',
            'requires_retry'
        )
    );

UPDATE users
SET onboarding_completed_at = COALESCE(onboarding_completed_at, created_at)
WHERE onboarding_completed_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_users_onboarding_completed_at
    ON users(onboarding_completed_at);

CREATE INDEX IF NOT EXISTS idx_users_identity_verification_status
    ON users(identity_verification_status);

CREATE INDEX IF NOT EXISTS idx_users_identity_verification_session_id
    ON users(identity_verification_session_id)
    WHERE identity_verification_session_id IS NOT NULL;
