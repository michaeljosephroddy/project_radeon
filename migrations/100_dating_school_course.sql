ALTER TABLE dating_profiles
    ADD COLUMN IF NOT EXISTS school TEXT,
    ADD COLUMN IF NOT EXISTS course TEXT;

ALTER TABLE dating_profiles
    DROP CONSTRAINT IF EXISTS dating_profiles_education_length_chk;

ALTER TABLE dating_profiles
    ADD CONSTRAINT dating_profiles_education_length_chk CHECK (
        char_length(COALESCE(education, '')) <= 170
    );

UPDATE dating_profiles
SET course = NULLIF(education, '')
WHERE NULLIF(course, '') IS NULL
  AND NULLIF(education, '') IS NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'dating_profiles_school_length_chk'
    ) THEN
        ALTER TABLE dating_profiles
            ADD CONSTRAINT dating_profiles_school_length_chk CHECK (
                char_length(COALESCE(school, '')) <= 80
            );
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'dating_profiles_course_length_chk'
    ) THEN
        ALTER TABLE dating_profiles
            ADD CONSTRAINT dating_profiles_course_length_chk CHECK (
                char_length(COALESCE(course, '')) <= 80
            );
    END IF;
END $$;
