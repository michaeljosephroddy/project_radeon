ALTER TABLE users
    ADD COLUMN IF NOT EXISTS subscription_tier TEXT NOT NULL DEFAULT 'free',
    ADD COLUMN IF NOT EXISTS subscription_status TEXT NOT NULL DEFAULT 'inactive';

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_subscription_tier_chk,
    DROP CONSTRAINT IF EXISTS users_subscription_status_chk;

ALTER TABLE users
    ADD CONSTRAINT users_subscription_tier_chk
        CHECK (subscription_tier IN ('free', 'plus')),
    ADD CONSTRAINT users_subscription_status_chk
        CHECK (subscription_status IN ('inactive', 'active', 'canceled', 'expired'));
