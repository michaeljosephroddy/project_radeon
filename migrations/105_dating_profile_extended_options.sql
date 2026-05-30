ALTER TABLE dating_profiles
    ADD COLUMN IF NOT EXISTS zodiac TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS family_plans TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS communication_style TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS love_style TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS workout TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS social_media TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS sober_lifestyle TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS recovery_approach TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS nightlife_comfort TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS substance_boundaries TEXT NOT NULL DEFAULT '';

ALTER TABLE dating_profiles
    DROP CONSTRAINT IF EXISTS dating_profiles_zodiac_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_family_plans_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_communication_style_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_love_style_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_workout_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_social_media_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_sober_lifestyle_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_recovery_approach_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_nightlife_comfort_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_substance_boundaries_chk;

ALTER TABLE dating_profiles
    ADD CONSTRAINT dating_profiles_zodiac_chk CHECK (
        zodiac IN ('', 'aries', 'taurus', 'gemini', 'cancer', 'leo', 'virgo', 'libra', 'scorpio', 'sagittarius', 'capricorn', 'aquarius', 'pisces')
    ),
    ADD CONSTRAINT dating_profiles_family_plans_chk CHECK (
        family_plans IN ('', 'want_children', 'dont_want_children', 'open_to_children', 'not_sure', 'prefer_not_to_say')
    ),
    ADD CONSTRAINT dating_profiles_communication_style_chk CHECK (
        communication_style IN ('', 'big_time_texter', 'phone_caller', 'video_chatter', 'bad_texter', 'better_in_person')
    ),
    ADD CONSTRAINT dating_profiles_love_style_chk CHECK (
        love_style IN ('', 'thoughtful_gestures', 'quality_time', 'words_of_affirmation', 'physical_touch', 'acts_of_service')
    ),
    ADD CONSTRAINT dating_profiles_workout_chk CHECK (
        workout IN ('', 'every_day', 'often', 'sometimes', 'occasionally', 'never')
    ),
    ADD CONSTRAINT dating_profiles_social_media_chk CHECK (
        social_media IN ('', 'influencer_status', 'socially_active', 'passive_scroller', 'off_the_grid')
    ),
    ADD CONSTRAINT dating_profiles_sober_lifestyle_chk CHECK (
        sober_lifestyle IN ('', 'sober', 'sober_curious', 'in_recovery', 'supportive_ally')
    ),
    ADD CONSTRAINT dating_profiles_recovery_approach_chk CHECK (
        recovery_approach IN ('', 'meetings', 'therapy', 'community', 'private', 'spiritual', 'self_guided')
    ),
    ADD CONSTRAINT dating_profiles_nightlife_comfort_chk CHECK (
        nightlife_comfort IN ('', 'dry_spaces_only', 'calm_venues', 'okay_with_bars', 'depends_on_company', 'prefer_daytime')
    ),
    ADD CONSTRAINT dating_profiles_substance_boundaries_chk CHECK (
        substance_boundaries IN ('', 'no_substances_around_me', 'no_drugs', 'no_smoking', 'ask_me_first', 'flexible')
    );

