CREATE INDEX IF NOT EXISTS idx_recovery_meetings_active_lat_lng
    ON recovery_meetings(latitude, longitude)
    WHERE status = 'active'
        AND latitude IS NOT NULL
        AND longitude IS NOT NULL;
