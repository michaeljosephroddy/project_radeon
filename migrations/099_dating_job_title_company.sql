ALTER TABLE dating_profiles
    ADD COLUMN IF NOT EXISTS job_title TEXT,
    ADD COLUMN IF NOT EXISTS company TEXT;

ALTER TABLE dating_profiles
    DROP CONSTRAINT IF EXISTS dating_profiles_work_length_chk;

ALTER TABLE dating_profiles
    ADD CONSTRAINT dating_profiles_work_length_chk CHECK (
        char_length(COALESCE(work, '')) <= 170
    );

UPDATE dating_profiles
SET job_title = NULLIF(work, '')
WHERE NULLIF(job_title, '') IS NULL
  AND NULLIF(work, '') IS NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'dating_profiles_job_title_length_chk'
    ) THEN
        ALTER TABLE dating_profiles
            ADD CONSTRAINT dating_profiles_job_title_length_chk CHECK (
                char_length(COALESCE(job_title, '')) <= 80
            );
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'dating_profiles_company_length_chk'
    ) THEN
        ALTER TABLE dating_profiles
            ADD CONSTRAINT dating_profiles_company_length_chk CHECK (
                char_length(COALESCE(company, '')) <= 80
            );
    END IF;
END $$;
