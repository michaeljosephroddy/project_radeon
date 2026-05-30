ALTER TABLE dating_profiles
    ADD COLUMN IF NOT EXISTS drinking_status TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS smoking_status TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS drug_use_status TEXT NOT NULL DEFAULT '';

ALTER TABLE dating_profiles
    DROP CONSTRAINT IF EXISTS dating_profiles_drinking_status_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_smoking_status_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_drug_use_status_chk;

ALTER TABLE dating_profiles
    ADD CONSTRAINT dating_profiles_drinking_status_chk CHECK (
        drinking_status IN ('', 'yes', 'sometimes', 'no', 'prefer_not_to_say')
    ),
    ADD CONSTRAINT dating_profiles_smoking_status_chk CHECK (
        smoking_status IN ('', 'yes', 'sometimes', 'no', 'prefer_not_to_say')
    ),
    ADD CONSTRAINT dating_profiles_drug_use_status_chk CHECK (
        drug_use_status IN ('', 'yes', 'sometimes', 'no', 'prefer_not_to_say')
    );
