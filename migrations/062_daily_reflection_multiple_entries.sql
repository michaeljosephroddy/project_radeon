ALTER TABLE daily_reflections
    DROP CONSTRAINT IF EXISTS daily_reflections_user_id_reflection_date_key;

DROP INDEX IF EXISTS idx_daily_reflections_user_date_desc;

CREATE INDEX IF NOT EXISTS idx_daily_reflections_user_date_desc
    ON daily_reflections(user_id, reflection_date DESC, id DESC);
