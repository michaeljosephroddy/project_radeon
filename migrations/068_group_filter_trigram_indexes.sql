CREATE INDEX IF NOT EXISTS idx_groups_country_trgm
    ON groups USING GIN(country gin_trgm_ops)
    WHERE deleted_at IS NULL AND country IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_groups_city_trgm
    ON groups USING GIN(city gin_trgm_ops)
    WHERE deleted_at IS NULL AND city IS NOT NULL;
