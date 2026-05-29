-- Simplify Groups v1 to public communities only.

DROP TABLE IF EXISTS group_invites;
DROP TABLE IF EXISTS group_join_requests;

UPDATE groups
SET visibility = 'public'
WHERE visibility <> 'public';

ALTER TABLE groups
    DROP CONSTRAINT IF EXISTS groups_visibility_chk;

ALTER TABLE groups
    ADD CONSTRAINT groups_visibility_chk CHECK (visibility = 'public');

ALTER TABLE groups
    DROP COLUMN IF EXISTS pending_request_count;

ALTER TABLE group_memberships
    DROP COLUMN IF EXISTS invited_by;
