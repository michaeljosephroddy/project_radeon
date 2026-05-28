DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM chats
        WHERE status NOT IN ('active', 'closed')
    ) THEN
        RAISE EXCEPTION 'cannot constrain chats.status: unsupported status values exist';
    END IF;
END $$;

ALTER TABLE chats
    DROP CONSTRAINT IF EXISTS chats_status_chk,
    DROP CONSTRAINT IF EXISTS chats_status_check,
    DROP CONSTRAINT IF EXISTS conversations_status_chk,
    DROP CONSTRAINT IF EXISTS conversations_status_check;

ALTER TABLE chats
    ADD CONSTRAINT chats_status_chk
        CHECK (status IN ('active', 'closed'));
