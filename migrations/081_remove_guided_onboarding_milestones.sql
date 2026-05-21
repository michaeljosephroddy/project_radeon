ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_onboarding_first_friend_user_id_fkey,
    DROP CONSTRAINT IF EXISTS users_onboarding_first_group_id_fkey,
    DROP CONSTRAINT IF EXISTS users_onboarding_first_post_id_fkey,
    DROP CONSTRAINT IF EXISTS users_onboarding_first_meetup_id_fkey,
    DROP CONSTRAINT IF EXISTS users_onboarding_first_dating_like_user_id_fkey;

ALTER TABLE users
    DROP COLUMN IF EXISTS onboarding_first_friend_user_id,
    DROP COLUMN IF EXISTS onboarding_first_group_id,
    DROP COLUMN IF EXISTS onboarding_first_post_id,
    DROP COLUMN IF EXISTS onboarding_first_meetup_id,
    DROP COLUMN IF EXISTS onboarding_first_dating_like_user_id;
