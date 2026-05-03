ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS is_system BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS system_key TEXT NULL,
    ADD COLUMN IF NOT EXISTS locked_settings BOOLEAN NOT NULL DEFAULT FALSE;

CREATE UNIQUE INDEX IF NOT EXISTS groups_system_key_unique_idx
    ON groups(system_key)
    WHERE system_key IS NOT NULL;

ALTER TABLE group_posts
    ADD COLUMN IF NOT EXISTS support_request_id UUID NULL REFERENCES support_requests(id) ON DELETE SET NULL;

ALTER TABLE support_requests
    ADD COLUMN IF NOT EXISTS group_post_id UUID NULL REFERENCES group_posts(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX IF NOT EXISTS group_posts_support_request_id_unique_idx
    ON group_posts(support_request_id)
    WHERE support_request_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS support_requests_group_post_id_unique_idx
    ON support_requests(group_post_id)
    WHERE group_post_id IS NOT NULL;

DO $$
DECLARE
    support_owner_id UUID;
    support_group_id UUID;
BEGIN
    SELECT id
    INTO support_owner_id
    FROM users
    ORDER BY created_at ASC, id ASC
    LIMIT 1;

    IF support_owner_id IS NOT NULL THEN
        INSERT INTO groups (
            owner_id,
            name,
            slug,
            description,
            rules,
            visibility,
            posting_permission,
            allow_anonymous_posts,
            tags,
            recovery_pathways,
            is_system,
            system_key,
            locked_settings
        )
        VALUES (
            support_owner_id,
            'Community Support',
            'system-community-support',
            'A community-wide support space for help requests and peer replies.',
            'Be kind, stay recovery-focused, and use private offers only when you can genuinely help.',
            'public',
            'members',
            FALSE,
            ARRAY['support', 'community'],
            ARRAY[]::TEXT[],
            TRUE,
            'community_support',
            TRUE
        )
        ON CONFLICT (system_key) WHERE system_key IS NOT NULL
        DO UPDATE SET
            name = EXCLUDED.name,
            description = EXCLUDED.description,
            rules = EXCLUDED.rules,
            visibility = 'public',
            posting_permission = 'members',
            is_system = TRUE,
            locked_settings = TRUE,
            deleted_at = NULL,
            updated_at = NOW()
        RETURNING id INTO support_group_id;

        INSERT INTO group_memberships (group_id, user_id, role, status, joined_at)
        SELECT support_group_id, u.id, 'member', 'active', NOW()
        FROM users u
        ON CONFLICT (group_id, user_id) DO NOTHING;

        UPDATE groups
        SET member_count = (
                SELECT COUNT(*)
                FROM group_memberships gm
                WHERE gm.group_id = support_group_id
                    AND gm.status = 'active'
            ),
            updated_at = NOW()
        WHERE id = support_group_id;

        WITH inserted_posts AS (
            INSERT INTO group_posts (
                group_id,
                user_id,
                post_type,
                body,
                anonymous,
                support_request_id,
                created_at,
                updated_at,
                comment_count
            )
            SELECT
                support_group_id,
                sr.requester_id,
                'need_support',
                LEFT(COALESCE(NULLIF(BTRIM(sr.message), ''), 'Support request'), 4000),
                FALSE,
                sr.id,
                sr.created_at,
                sr.created_at,
                COALESCE(sr.reply_count, 0)
            FROM support_requests sr
            WHERE sr.group_post_id IS NULL
                AND NOT EXISTS (
                    SELECT 1
                    FROM group_posts gp
                    WHERE gp.support_request_id = sr.id
                )
            RETURNING id, support_request_id
        )
        UPDATE support_requests sr
        SET group_post_id = ip.id
        FROM inserted_posts ip
        WHERE sr.id = ip.support_request_id;

        UPDATE support_requests sr
        SET group_post_id = gp.id
        FROM group_posts gp
        WHERE gp.support_request_id = sr.id
            AND sr.group_post_id IS NULL;

        UPDATE group_posts gp
        SET support_request_id = sr.id
        FROM support_requests sr
        WHERE sr.group_post_id = gp.id
            AND gp.support_request_id IS NULL;

        INSERT INTO group_comments (
            id,
            group_id,
            post_id,
            user_id,
            body,
            created_at,
            updated_at
        )
        SELECT
            reply.id,
            gp.group_id,
            gp.id,
            reply.author_id,
            LEFT(COALESCE(NULLIF(BTRIM(reply.body), ''), 'Reply'), 2000),
            reply.created_at,
            reply.updated_at
        FROM support_replies reply
        JOIN support_requests sr ON sr.id = reply.support_request_id
        JOIN group_posts gp ON gp.id = sr.group_post_id
        WHERE gp.group_id = support_group_id
            AND NOT EXISTS (
                SELECT 1
                FROM group_comments gc
                WHERE gc.id = reply.id
            );

        UPDATE group_posts gp
        SET comment_count = (
                SELECT COUNT(*)
                FROM group_comments gc
                WHERE gc.post_id = gp.id
                    AND gc.deleted_at IS NULL
            ),
            updated_at = NOW()
        WHERE gp.group_id = support_group_id
            AND gp.support_request_id IS NOT NULL;

        UPDATE groups
        SET post_count = (
                SELECT COUNT(*)
                FROM group_posts gp
                WHERE gp.group_id = support_group_id
                    AND gp.deleted_at IS NULL
            ),
            updated_at = NOW()
        WHERE id = support_group_id;
    END IF;
END $$;
