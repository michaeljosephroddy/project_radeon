CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_country_region_city
    ON recovery_meetings(country, region, city)
    WHERE status = 'active';
