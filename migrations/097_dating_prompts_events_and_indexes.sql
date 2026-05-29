CREATE TABLE IF NOT EXISTS dating_profile_prompt_answers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id UUID NOT NULL REFERENCES dating_profiles(id) ON DELETE CASCADE,
    prompt_key TEXT NOT NULL,
    answer TEXT NOT NULL,
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(profile_id, prompt_key),
    CONSTRAINT dating_profile_prompt_key_chk CHECK (
        prompt_key IN (
            'ideal_sober_date',
            'sober_weekend',
            'recovery_lifestyle',
            'looking_for'
        )
    ),
    CONSTRAINT dating_profile_prompt_answer_length_chk CHECK (
        length(trim(answer)) BETWEEN 1 AND 220
    ),
    CONSTRAINT dating_profile_prompt_position_chk CHECK (position BETWEEN 0 AND 3)
);

CREATE INDEX IF NOT EXISTS idx_dating_profile_prompt_answers_profile_position
    ON dating_profile_prompt_answers(profile_id, position);

CREATE TABLE IF NOT EXISTS dating_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    profile_id UUID REFERENCES dating_profiles(id) ON DELETE SET NULL,
    match_id UUID REFERENCES dating_matches(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    position INT,
    event_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    payload JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT dating_events_event_type_chk CHECK (
        event_type IN (
            'setup_started',
            'setup_completed',
            'profile_opened',
            'like',
            'pass',
            'match_created',
            'chat_opened',
            'first_message_sent',
            'report',
            'block',
            'unmatch',
            'likes_you_gate_viewed'
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_dating_events_user_event_at
    ON dating_events(user_id, event_at DESC);

CREATE INDEX IF NOT EXISTS idx_dating_events_profile_event_at
    ON dating_events(profile_id, event_at DESC)
    WHERE profile_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_dating_events_match_event_at
    ON dating_events(match_id, event_at DESC)
    WHERE match_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_dating_events_type_event_at
    ON dating_events(event_type, event_at DESC);

CREATE INDEX IF NOT EXISTS idx_dating_profiles_user_completed_paused
    ON dating_profiles(user_id, completed_at, paused);

CREATE INDEX IF NOT EXISTS idx_dating_actions_target_action_updated
    ON dating_actions(target_id, action, updated_at DESC);
