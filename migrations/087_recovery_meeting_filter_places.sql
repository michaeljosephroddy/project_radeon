CREATE OR REPLACE VIEW recovery_meeting_filter_places AS
WITH normalized AS (
    SELECT
        rm.id AS meeting_id,
        rm.fellowship,
        rm.meeting_type,
        NULLIF(TRIM(COALESCE(rm.country, '')), '') AS country,
        NULLIF(TRIM(COALESCE(rm.country_code, '')), '') AS country_code,
        NULLIF(TRIM(COALESCE(rm.region, '')), '') AS raw_region,
        NULLIF(TRIM(COALESCE(rm.region_code, '')), '') AS region_code,
        NULLIF(TRIM(COALESCE(rm.city, '')), '') AS raw_city,
        CASE
            WHEN TRIM(COALESCE(rm.region, '')) = ''
                AND TRIM(COALESCE(rm.city, '')) ~* '^(co\.?|county|state|province|prov\.?|region|prefecture|department)\s+'
            THEN NULLIF(
                regexp_replace(
                    regexp_replace(
                        TRIM(COALESCE(rm.city, '')),
                        '^(co\.?|county|state|province|prov\.?|region|prefecture|department)\s+(of\s+)?',
                        '',
                        'i'
                    ),
                    '\s+(north|south|east|west)$',
                    '',
                    'i'
                ),
                ''
            )
            ELSE NULL
        END AS derived_admin_area,
        NULLIF(TRIM(COALESCE(rm.venue_name, '')), '') AS venue_name,
        NULLIF(TRIM(COALESCE(rm.address_line1, '')), '') AS address_line1,
        NULLIF(TRIM(COALESCE(rm.address_line2, '')), '') AS address_line2,
        NULLIF(TRIM(COALESCE(rm.postal_code, '')), '') AS postal_code
    FROM recovery_meetings rm
    WHERE rm.status = 'active'
)
SELECT
    meeting_id,
    fellowship,
    meeting_type,
    country,
    country_code,
    COALESCE(raw_region, derived_admin_area) AS region,
    region_code,
    COALESCE(derived_admin_area, raw_city) AS locality,
    CONCAT_WS(
        ' ',
        country,
        country_code,
        COALESCE(raw_region, derived_admin_area),
        region_code,
        COALESCE(derived_admin_area, raw_city),
        raw_city,
        venue_name,
        address_line1,
        address_line2,
        postal_code
    ) AS search_text
FROM normalized;
