CREATE TABLE IF NOT EXISTS dating_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (actor_id <> target_id),
    CONSTRAINT dating_actions_action_chk CHECK (action IN ('like', 'pass')),
    UNIQUE (actor_id, target_id)
);

CREATE INDEX IF NOT EXISTS idx_dating_actions_actor_updated
    ON dating_actions(actor_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_dating_actions_target_like
    ON dating_actions(target_id, actor_id)
    WHERE action = 'like';

CREATE TABLE IF NOT EXISTS dating_matches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_a_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_b_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    chat_id UUID REFERENCES chats(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'active',
    matched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    unmatched_at TIMESTAMPTZ,
    unmatched_by UUID REFERENCES users(id) ON DELETE SET NULL,
    CHECK (user_a_id < user_b_id),
    CONSTRAINT dating_matches_status_chk CHECK (status IN ('active', 'unmatched')),
    CONSTRAINT dating_matches_unmatch_chk CHECK (
        (status = 'active' AND unmatched_at IS NULL AND unmatched_by IS NULL)
        OR (status = 'unmatched' AND unmatched_at IS NOT NULL AND unmatched_by IS NOT NULL)
    ),
    UNIQUE (user_a_id, user_b_id)
);

CREATE INDEX IF NOT EXISTS idx_dating_matches_user_a_status
    ON dating_matches(user_a_id, status, matched_at DESC);

CREATE INDEX IF NOT EXISTS idx_dating_matches_user_b_status
    ON dating_matches(user_b_id, status, matched_at DESC);

CREATE INDEX IF NOT EXISTS idx_dating_matches_chat_id
    ON dating_matches(chat_id);
