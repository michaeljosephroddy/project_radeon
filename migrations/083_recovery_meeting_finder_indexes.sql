CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_filters
    ON recovery_meetings(fellowship, country, meeting_type)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_name_sort
    ON recovery_meetings(LOWER(name), id)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_name_trgm
    ON recovery_meetings USING GIN(name gin_trgm_ops)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_city_trgm
    ON recovery_meetings USING GIN(city gin_trgm_ops)
    WHERE status = 'active' AND city IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_region_trgm
    ON recovery_meetings USING GIN(region gin_trgm_ops)
    WHERE status = 'active' AND region IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_country_trgm
    ON recovery_meetings USING GIN(country gin_trgm_ops)
    WHERE status = 'active' AND country IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_venue_trgm
    ON recovery_meetings USING GIN(venue_name gin_trgm_ops)
    WHERE status = 'active' AND venue_name IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_address1_trgm
    ON recovery_meetings USING GIN(address_line1 gin_trgm_ops)
    WHERE status = 'active' AND address_line1 IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_address2_trgm
    ON recovery_meetings USING GIN(address_line2 gin_trgm_ops)
    WHERE status = 'active' AND address_line2 IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_postal_trgm
    ON recovery_meetings USING GIN(postal_code gin_trgm_ops)
    WHERE status = 'active' AND postal_code IS NOT NULL;
