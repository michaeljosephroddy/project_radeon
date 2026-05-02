ALTER TABLE posts
    DROP CONSTRAINT IF EXISTS posts_source_type_chk;

DROP TABLE IF EXISTS daily_reflections;
