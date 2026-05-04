CREATE TABLE IF NOT EXISTS group_admin_thread_reads (
    thread_id UUID NOT NULL REFERENCES group_admin_threads(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    last_read_message_id UUID REFERENCES group_admin_messages(id) ON DELETE SET NULL,
    last_read_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (thread_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_group_admin_thread_reads_user_id
    ON group_admin_thread_reads(user_id);

INSERT INTO group_admin_thread_reads (thread_id, user_id, last_read_message_id, last_read_at)
SELECT
    gat.id,
    gm.user_id,
    latest.id,
    NOW()
FROM group_admin_threads gat
JOIN group_memberships gm
    ON gm.group_id = gat.group_id
    AND gm.status = 'active'
    AND gm.role IN ('owner', 'admin', 'moderator')
LEFT JOIN LATERAL (
    SELECT gam.id
    FROM group_admin_messages gam
    WHERE gam.thread_id = gat.id
    ORDER BY gam.created_at DESC
    LIMIT 1
) latest ON TRUE
ON CONFLICT (thread_id, user_id) DO NOTHING;
