ALTER TABLE messages
ADD COLUMN IF NOT EXISTS client_message_id TEXT,
ADD COLUMN IF NOT EXISTS chat_seq BIGINT;

WITH ranked_messages AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY chat_id
            ORDER BY sent_at ASC, id ASC
        ) AS next_chat_seq
    FROM messages
)
UPDATE messages AS m
SET chat_seq = ranked_messages.next_chat_seq
FROM ranked_messages
WHERE m.id = ranked_messages.id
  AND m.chat_seq IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_chat_id_client_message_id
    ON messages(chat_id, client_message_id)
    WHERE client_message_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_chat_id_chat_seq
    ON messages(chat_id, chat_seq)
    WHERE chat_seq IS NOT NULL;
