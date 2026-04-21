ALTER TABLE chats
ADD COLUMN IF NOT EXISTS support_request_id UUID REFERENCES support_requests(id) ON DELETE SET NULL;

ALTER TABLE support_responses
ADD COLUMN IF NOT EXISTS scheduled_for TIMESTAMP NULL,
ADD COLUMN IF NOT EXISTS chat_id UUID REFERENCES chats(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_chats_support_request_id
ON chats(support_request_id);

CREATE INDEX IF NOT EXISTS idx_support_responses_chat_id
ON support_responses(chat_id);
