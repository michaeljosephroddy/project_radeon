ALTER TABLE group_admin_threads
    DROP CONSTRAINT IF EXISTS group_admin_threads_status_chk;

ALTER TABLE group_admin_threads
    ADD CONSTRAINT group_admin_threads_status_chk
    CHECK (status IN ('open', 'replied', 'resolved'));
