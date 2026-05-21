ALTER TABLE users
    ADD COLUMN IF NOT EXISTS onboarding_first_meetup_id UUID NULL REFERENCES meetups(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS onboarding_first_dating_like_user_id UUID NULL REFERENCES users(id) ON DELETE SET NULL;
