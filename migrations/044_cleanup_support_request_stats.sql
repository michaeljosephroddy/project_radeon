ALTER TABLE support_responder_presence
    DROP COLUMN IF EXISTS active_session_count;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'support_responder_stats'
          AND column_name = 'last_session_completed_at'
    ) THEN
        ALTER TABLE support_responder_stats
            RENAME COLUMN last_session_completed_at TO last_request_completed_at;
    END IF;
END $$;

ALTER TABLE support_responder_stats
    DROP COLUMN IF EXISTS total_completed_sessions,
    DROP COLUMN IF EXISTS total_cancelled_sessions;
