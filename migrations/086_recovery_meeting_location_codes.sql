ALTER TABLE recovery_meetings
    ADD COLUMN IF NOT EXISTS region_code TEXT,
    ADD COLUMN IF NOT EXISTS country_code TEXT;

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_country_code_region_code_city
    ON recovery_meetings(country_code, region_code, city)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_region_code
    ON recovery_meetings(region_code)
    WHERE status = 'active' AND region_code IS NOT NULL;
