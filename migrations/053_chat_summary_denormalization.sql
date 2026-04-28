ALTER TABLE chats
ADD COLUMN IF NOT EXISTS next_message_seq BIGINT NOT NULL DEFAULT 1,
ADD COLUMN IF NOT EXISTS last_message_id UUID REFERENCES messages(id) ON DELETE SET NULL,
ADD COLUMN IF NOT EXISTS last_message_sender_id UUID REFERENCES users(id) ON DELETE SET NULL,
ADD COLUMN IF NOT EXISTS last_message_body TEXT,
ADD COLUMN IF NOT EXISTS last_message_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS last_message_seq BIGINT NOT NULL DEFAULT 0;

ALTER TABLE chat_reads
ADD COLUMN IF NOT EXISTS last_read_chat_seq BIGINT NOT NULL DEFAULT 0;

WITH latest_messages AS (
    SELECT DISTINCT ON (m.chat_id)
        m.chat_id,
        m.id,
        m.sender_id,
        m.body,
        m.sent_at,
        m.chat_seq
    FROM messages m
    ORDER BY m.chat_id, m.chat_seq DESC, m.sent_at DESC, m.id DESC
),
max_sequences AS (
    SELECT
        m.chat_id,
        MAX(m.chat_seq) AS max_chat_seq
    FROM messages m
    GROUP BY m.chat_id
)
UPDATE chats ch
SET next_message_seq = COALESCE(max_sequences.max_chat_seq, 0) + 1,
    last_message_id = latest_messages.id,
    last_message_sender_id = latest_messages.sender_id,
    last_message_body = latest_messages.body,
    last_message_at = latest_messages.sent_at,
    last_message_seq = COALESCE(max_sequences.max_chat_seq, 0)
FROM max_sequences
LEFT JOIN latest_messages
    ON latest_messages.chat_id = max_sequences.chat_id
WHERE ch.id = max_sequences.chat_id;

UPDATE chat_reads cr
SET last_read_chat_seq = COALESCE(m.chat_seq, 0)
FROM messages m
WHERE cr.last_read_message_id = m.id;

CREATE INDEX IF NOT EXISTS idx_chats_last_message_at
    ON chats(last_message_at DESC);

CREATE INDEX IF NOT EXISTS idx_chats_last_message_sender_id
    ON chats(last_message_sender_id);
