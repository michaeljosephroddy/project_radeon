DELETE FROM meetups
WHERE status = 'draft';

UPDATE meetups
SET published_at = COALESCE(published_at, created_at, NOW())
WHERE status = 'published'
  AND published_at IS NULL;

ALTER TABLE meetups
    DROP CONSTRAINT IF EXISTS meetups_status_chk;

ALTER TABLE meetups
    ADD CONSTRAINT meetups_status_chk
    CHECK (status IN ('published', 'cancelled', 'completed'));
