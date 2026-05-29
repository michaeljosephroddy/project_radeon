ALTER TABLE dating_profiles
    ADD COLUMN IF NOT EXISTS height_cm INT,
    ADD COLUMN IF NOT EXISTS work TEXT,
    ADD COLUMN IF NOT EXISTS education TEXT,
    ADD COLUMN IF NOT EXISTS kids_status TEXT NOT NULL DEFAULT '';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'dating_profiles_height_cm_chk'
    ) THEN
        ALTER TABLE dating_profiles
            ADD CONSTRAINT dating_profiles_height_cm_chk CHECK (
                height_cm IS NULL OR height_cm BETWEEN 90 AND 230
            );
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'dating_profiles_work_length_chk'
    ) THEN
        ALTER TABLE dating_profiles
            ADD CONSTRAINT dating_profiles_work_length_chk CHECK (
                char_length(COALESCE(work, '')) <= 80
            );
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'dating_profiles_education_length_chk'
    ) THEN
        ALTER TABLE dating_profiles
            ADD CONSTRAINT dating_profiles_education_length_chk CHECK (
                char_length(COALESCE(education, '')) <= 80
            );
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'dating_profiles_kids_status_chk'
    ) THEN
        ALTER TABLE dating_profiles
            ADD CONSTRAINT dating_profiles_kids_status_chk CHECK (
                kids_status IN ('', 'have_kids', 'dont_have_kids', 'prefer_not_to_say')
            );
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS dating_profile_interests (
    profile_id UUID NOT NULL REFERENCES dating_profiles(id) ON DELETE CASCADE,
    interest_id UUID NOT NULL REFERENCES interests(id) ON DELETE CASCADE,
    PRIMARY KEY (profile_id, interest_id)
);

CREATE INDEX IF NOT EXISTS idx_dating_profile_interests_interest_id
    ON dating_profile_interests(interest_id);

INSERT INTO dating_profile_interests (profile_id, interest_id)
SELECT dp.id, ui.interest_id
FROM dating_profiles dp
JOIN user_interests ui ON ui.user_id = dp.user_id
ON CONFLICT (profile_id, interest_id) DO NOTHING;
