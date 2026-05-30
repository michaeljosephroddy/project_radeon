CREATE TABLE IF NOT EXISTS support_signals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason TEXT NOT NULL,
    note TEXT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    expires_at TIMESTAMPTZ NOT NULL,
    response_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ NULL,
    cancelled_at TIMESTAMPTZ NULL,
    CONSTRAINT support_signals_reason_check CHECK (
        reason IN ('cravings', 'relapse_risk', 'overwhelmed', 'lonely', 'risky_place', 'need_to_talk')
    ),
    CONSTRAINT support_signals_status_check CHECK (
        status IN ('active', 'resolved', 'cancelled', 'expired')
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS support_signals_one_active_per_user_idx
    ON support_signals(user_id)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_support_signals_active_expires_created
    ON support_signals(status, expires_at DESC, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_signals_user_status_created
    ON support_signals(user_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS support_signal_responses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    signal_id UUID NOT NULL REFERENCES support_signals(id) ON DELETE CASCADE,
    responder_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message TEXT NULL,
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT support_signal_responses_unique_responder UNIQUE (signal_id, responder_id)
);

CREATE INDEX IF NOT EXISTS idx_support_signal_responses_signal_created
    ON support_signal_responses(signal_id, created_at DESC);

ALTER TABLE notification_preferences
    ADD COLUMN IF NOT EXISTS reach_out_alerts BOOLEAN NOT NULL DEFAULT TRUE;

ALTER TABLE notification_preferences
    ADD COLUMN IF NOT EXISTS reach_out_helper_alerts BOOLEAN NOT NULL DEFAULT FALSE;
